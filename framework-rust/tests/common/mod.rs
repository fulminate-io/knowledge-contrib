// SPDX-License-Identifier: Apache-2.0

//! common/mod.rs — a minimal MCP stdio client, so a test can drive one of this
//! crate's binaries the way the knowledge daemon does.
//!
//! IT SPEAKS THE WIRE RATHER THAN CALLING THE LIBRARY, deliberately. Half the
//! properties in this crate's suite are about what reaches the wire — the
//! advertised schemas verbatim, the negotiated revision, `nodes` as `[]` and not
//! `null`, a 68 MiB result surviving the transport, a process that dies inside a
//! call. None of those is observable from a function return.

#![allow(dead_code)]

use std::io::{BufRead, BufReader, Write};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::sync::mpsc;
use std::thread;

use serde_json::{json, Value};

/// The revision this client REQUESTS. It is deliberately newer than the one the
/// crate pins, because that is what the knowledge client does: its own SDK asks
/// for the newest revision it knows and accepts several down.
pub const CLIENT_REQUESTED_PROTOCOL_VERSION: &str = "2026-07-28";

/// McpChild is one of this crate's binaries, spawned and spoken to over stdio.
pub struct McpChild {
    child: Child,
    stdin: Option<ChildStdin>,
    stdout: BufReader<ChildStdout>,
    stderr: mpsc::Receiver<String>,
    next_id: i64,
}

impl McpChild {
    /// spawn starts a binary with an EXPLICIT environment: nothing of the test
    /// process's own environment reaches it unless a caller names it. That is the
    /// same posture the client's entry env block has, and it is what makes the
    /// environment-probe modes mean anything.
    pub fn spawn(exe: &str, env: &[(&str, &str)]) -> McpChild {
        let mut cmd = Command::new(exe);
        cmd.env_clear();
        // PATH is carried because a child that cannot resolve the dynamic linker
        // on some platforms fails before main; nothing else is.
        if let Ok(path) = std::env::var("PATH") {
            cmd.env("PATH", path);
        }
        for (k, v) in env {
            cmd.env(k, v);
        }
        let mut child = cmd
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap_or_else(|e| panic!("spawning {exe}: {e}"));

        let stdin = child.stdin.take().expect("piped stdin");
        let stdout = BufReader::new(child.stdout.take().expect("piped stdout"));
        let child_stderr = child.stderr.take().expect("piped stderr");
        let (tx, rx) = mpsc::channel();
        thread::spawn(move || {
            for line in BufReader::new(child_stderr).lines().map_while(Result::ok) {
                let _ = tx.send(line);
            }
        });

        McpChild {
            child,
            stdin: Some(stdin),
            stdout,
            stderr: rx,
            next_id: 0,
        }
    }

    /// send_raw writes one line to the child's stdin.
    pub fn send_raw(&mut self, line: &str) {
        let stdin = self
            .stdin
            .as_mut()
            .expect("the child's stdin is still open");
        writeln!(stdin, "{line}").expect("writing a frame to the child");
        stdin.flush().expect("flushing the child's stdin");
    }

    /// read_line reads one raw line of the child's stdout, or None at EOF.
    pub fn read_line(&mut self) -> Option<String> {
        let mut buf = String::new();
        match self.stdout.read_line(&mut buf) {
            Ok(0) => None,
            Ok(_) => Some(buf.trim_end().to_string()),
            Err(_) => None,
        }
    }

    /// request sends one JSON-RPC request and returns the response frame whose
    /// id matches, skipping notifications the server sends in between.
    pub fn request(&mut self, method: &str, params: Value) -> Value {
        self.next_id += 1;
        let id = self.next_id;
        let frame = json!({"jsonrpc": "2.0", "id": id, "method": method, "params": params});
        self.send_raw(&serde_json::to_string(&frame).expect("a frame serializes"));
        loop {
            let Some(line) = self.read_line() else {
                panic!(
                    "the child closed its stdout before answering {method}; its stderr was:\n{}",
                    self.drain_stderr().join("\n")
                );
            };
            if line.is_empty() {
                continue;
            }
            let value: Value = serde_json::from_str(&line).unwrap_or_else(|e| {
                panic!("the child wrote a line that is not JSON-RPC ({e}): {line}")
            });
            if value.get("id").and_then(Value::as_i64) == Some(id) {
                return value;
            }
        }
    }

    /// notify sends one JSON-RPC notification.
    pub fn notify(&mut self, method: &str, params: Value) {
        let frame = json!({"jsonrpc": "2.0", "method": method, "params": params});
        self.send_raw(&serde_json::to_string(&frame).expect("a frame serializes"));
    }

    /// initialize performs the handshake and returns the initialize RESULT.
    pub fn initialize(&mut self) -> Value {
        let response = self.request(
            "initialize",
            json!({
                "protocolVersion": CLIENT_REQUESTED_PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": {"name": "framework-rust-test-client", "version": "v1"}
            }),
        );
        let result = response
            .get("result")
            .unwrap_or_else(|| panic!("the handshake failed: {response}"))
            .clone();
        self.notify("notifications/initialized", json!({}));
        result
    }

    /// list_tools returns the `tools` array of a `tools/list`.
    pub fn list_tools(&mut self) -> Vec<Value> {
        let response = self.request("tools/list", json!({}));
        response
            .get("result")
            .and_then(|r| r.get("tools"))
            .and_then(Value::as_array)
            .unwrap_or_else(|| panic!("tools/list did not answer with a tools array: {response}"))
            .clone()
    }

    /// call_tool returns the whole `result` object of a `tools/call`.
    pub fn call_tool(&mut self, name: &str, arguments: Value) -> Value {
        let response = self.request(
            "tools/call",
            json!({"name": name, "arguments": arguments}),
        );
        response
            .get("result")
            .unwrap_or_else(|| panic!("tools/call answered with no result: {response}"))
            .clone()
    }

    /// call_tool_raw returns the whole response frame, so a test can read a
    /// protocol error rather than a tool result.
    pub fn call_tool_raw(&mut self, name: &str, arguments: Value) -> Value {
        self.request("tools/call", json!({"name": name, "arguments": arguments}))
    }

    /// drain_stderr collects every stderr line the child has written so far.
    pub fn drain_stderr(&mut self) -> Vec<String> {
        let mut out = Vec::new();
        while let Ok(line) = self.stderr.try_recv() {
            out.push(line);
        }
        out
    }

    /// wait_for_exit closes stdin and waits for the child, returning its exit
    /// code.
    pub fn wait_for_exit(mut self) -> Option<i32> {
        // Closing stdin is what lets a healthy child end its session; a child
        // that has already exited is unaffected.
        self.stdin.take();
        let status = self.child.wait().expect("waiting for the child");
        status.code()
    }

    /// kill stops the child.
    pub fn kill(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Drop for McpChild {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// is_error reports whether a `tools/call` result is a tool error.
pub fn is_error(result: &Value) -> bool {
    result.get("isError").and_then(Value::as_bool) == Some(true)
}

/// error_text concatenates the text content blocks of a tool result.
pub fn error_text(result: &Value) -> String {
    result
        .get("content")
        .and_then(Value::as_array)
        .map(|blocks| {
            blocks
                .iter()
                .filter_map(|b| b.get("text").and_then(Value::as_str))
                .collect::<Vec<_>>()
                .join("\n")
        })
        .unwrap_or_default()
}
