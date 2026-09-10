// endtoend.test.ts — R3 and R4.4: the sample collector registered by hand on an
// ISOLATED client and server pair and COLLECTED, with the four observables the
// ticket names asserted against the real client rather than against this
// package's own driver.
//
// WHY THIS FILE EXISTS SEPARATELY FROM sample.test.ts. Everything there runs the
// collector in process or through the package's own session; the requirement is
// about what happens when the CLIENT drives it. Registration alone is a dial and
// proves the schema gate; it proves nothing about the collect, and the collect is
// where walk_complete, the deletion phase, the node vocabulary and the error arm
// are decided.
//
// THE VENUE IS AN ISOLATED PAIR ON PICKED FREE PORTS UNDER A SCRATCH HOME. Every
// process is spawned with an explicit, complete environment rather than an
// inherited one, so nothing of the developer's machine reaches it. The operator's
// own daemon and graph store are never contacted: this row starts its own server
// and its own client and kills both by pid.
//
// THE MCP CALLS GO THROUGH curl, and that is deliberate rather than convenient.
// R4.1 requires that this package's runtime and test code open NO socket, and a
// row that imported node:http to drive the pair would make that claim false
// while leaving the scan green only by exempting itself. A subprocess talking to
// a server this test started is not the package opening a socket, and it is not
// the network access R4.1 is about.

import assert from "node:assert/strict";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import {
  closeSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import { packageRoot } from "./helpers.js";

type Json = Record<string, any>;

/** A port nothing is listening on, checked rather than assumed. */
function pickFreePort(from: number): number {
  for (let port = from; port < from + 200; port++) {
    const held = spawnSync("lsof", ["-nP", `-iTCP:${port}`, "-sTCP:LISTEN"], { encoding: "utf8" });
    // lsof exits non-zero when nothing matches, which is the free case.
    if (held.status !== 0) return port;
  }
  throw new Error(`no free port in [${from}, ${from + 200})`);
}

/**
 * The whole environment each spawned process gets. Nothing is inherited, and
 * NO PROVIDER CREDENTIAL IS SUPPLIED.
 *
 * AN EARLIER VERSION OF THIS ROW PUT A FAKE ANTHROPIC KEY HERE, reasoning that it
 * "reaches nothing" because it is not a real key. It reached Anthropic: with a
 * provider configured the client's precheck DIALS it at startup, and the run
 * logged a completed HTTPS round trip to the vendor carrying a real request id
 * and a 401, on every start of the pair. Ticket requirement R4 says the suite
 * makes no network call, and a fake credential is still a credential the test
 * handed to a process that then used it.
 *
 * IT ALSO BOUGHT THIS ROW NOTHING. The key existed to let the summarizer chain
 * resolve so the pipeline would wire, because BM25 population rides that wire —
 * and no assertion here reads search or BM25. This row asserts the collect, a
 * traverse, a query by id, the deletion pair and the throwing walk, every one of
 * them a graph read. So the client is started with --no-llm-pipeline instead, and
 * the row that asserts a search result is the one that may pay for a wire.
 */
function isolatedEnv(home: string): Record<string, string> {
  return {
    HOME: home,
    PATH: "/usr/bin:/bin:/usr/sbin:/sbin",
    TMPDIR: "/tmp",
  };
}

/** How much of each child's output is kept for a failure message. */
const LOG_TAIL_BYTES = 16 * 1024;

/**
 * One spawned process whose output goes to FILES rather than to pipes.
 *
 * WHY FILES, and it is not a preference. A pipe with no reader blocks its writer
 * once the buffer fills: measured, the client writes about 3.1 KB in twelve idle
 * seconds against an 8 KB socket buffer, and this row budgets sixty one-second
 * handshake retries and a 180 s timeout. A slower, chattier or retrying run
 * crosses the ceiling, the child blocks on a write nobody will read, and the row
 * hangs to its timeout instead of failing on an assertion — a CI flake with no
 * diagnostic, because nothing was reading the stream that would have said why.
 *
 * AND A PIPE WITH AN EVENT-HANDLER READER DOES NOT FIX IT HERE. This row's body
 * is synchronous from end to end: it drives the pair with spawnSync, so the event
 * loop never turns between the spawn and the assertions and a `data` handler is
 * never dispatched. Measured on the first attempt at this fix — both buffers were
 * empty at the first assertion, which is what the control below caught.
 *
 * A file has no such ceiling, so the writer never blocks, and it can be read
 * SYNCHRONOUSLY at any point, which is what lets a failure message carry the
 * child's own words. The tail rather than the head: a process that fails at
 * startup writes its reason and exits, so the tail holds everything.
 */
class LoggedChild {
  constructor(
    readonly name: string,
    readonly process: ChildProcess,
    private readonly outPath: string,
    private readonly errPath: string,
  ) {}

  get stderr(): string {
    return this.#read(this.errPath);
  }

  get bytesRead(): number {
    return this.#read(this.outPath).length + this.#read(this.errPath).length;
  }

  /** The tail of both streams, for a failure message. */
  diagnostic(): string {
    return (
      `\n--- ${this.name} stderr ---\n${this.#read(this.errPath)}` +
      `\n--- ${this.name} stdout ---\n${this.#read(this.outPath)}`
    );
  }

  #read(path: string): string {
    if (!existsSync(path)) return "";
    const text = readFileSync(path, "utf8");
    return text.length <= LOG_TAIL_BYTES ? text : text.slice(text.length - LOG_TAIL_BYTES);
  }
}

/** Spawns one child with its two streams redirected to files under `dir`. */
function spawnLogged(
  name: string,
  dir: string,
  command: string,
  args: string[],
  env: Record<string, string>,
): LoggedChild {
  const outPath = join(dir, `${name}.out.log`);
  const errPath = join(dir, `${name}.err.log`);
  const out = openSync(outPath, "a");
  const err = openSync(errPath, "a");
  try {
    const child = spawn(command, args, { env, stdio: ["ignore", out, err] });
    return new LoggedChild(name, child, outPath, errPath);
  } finally {
    // The child holds its own duplicates of both descriptors; these are this
    // process's, and leaving them open would leak one pair per row.
    closeSync(out);
    closeSync(err);
  }
}

/** One MCP tool call against the client's loopback endpoint, in one session. */
class McpSession {
  #id = 0;
  #session = "";

  constructor(
    private readonly port: number,
    private readonly scratch: string,
  ) {}

  /** Completes the handshake and the initialized notification. */
  open(): void {
    const init = this.#post({
      jsonrpc: "2.0",
      id: ++this.#id,
      method: "initialize",
      params: {
        protocolVersion: "2025-06-18",
        capabilities: {},
        clientInfo: { name: "kn-t37-endtoend", version: "v1" },
      },
    });
    assert.ok(init?.["result"] !== undefined, `the client refused the handshake: ${JSON.stringify(init)}`);
    this.#post({ jsonrpc: "2.0", method: "notifications/initialized" });
  }

  /** Calls one tool and returns the rendered text of its result. */
  call(tool: string, args: Json): { text: string; isError: boolean } {
    const reply = this.#post({
      jsonrpc: "2.0",
      id: ++this.#id,
      method: "tools/call",
      params: { name: tool, arguments: args },
    });
    assert.ok(reply !== undefined, `no reply to ${tool}`);
    const result = reply["result"] as Json | undefined;
    assert.ok(result !== undefined, `${tool} answered a protocol error: ${JSON.stringify(reply)}`);
    const content = (result["content"] ?? []) as Json[];
    return {
      text: content.map((c) => String(c["text"] ?? "")).join("\n"),
      isError: result["isError"] === true,
    };
  }

  #post(body: Json): Json | undefined {
    const headerFile = join(this.scratch, "mcp-headers.txt");
    const args = [
      "-sS",
      "-X",
      "POST",
      `http://127.0.0.1:${this.port}/mcp`,
      "-H",
      "Content-Type: application/json",
      "-H",
      "Accept: application/json, text/event-stream",
      "-D",
      headerFile,
      "--data-binary",
      JSON.stringify(body),
    ];
    if (this.#session !== "") args.push("-H", `Mcp-Session-Id: ${this.#session}`);
    const run = spawnSync("curl", args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
    assert.equal(run.status, 0, `curl failed: ${run.stderr}`);
    const headers = existsSync(headerFile) ? readFileSync(headerFile, "utf8") : "";
    const sid = /^mcp-session-id:\s*(\S+)/im.exec(headers);
    if (sid !== null) this.#session = sid[1]!;
    // A streamable-HTTP reply is either a JSON body or an SSE stream of them.
    for (const line of run.stdout.split("\n")) {
      const trimmed = line.startsWith("data:") ? line.slice(5).trim() : line.trim();
      if (!trimmed.startsWith("{")) continue;
      try {
        return JSON.parse(trimmed) as Json;
      } catch {
        continue;
      }
    }
    return undefined;
  }
}

test("R3 and R4.4 the sample is registered and COLLECTED by the real client on an isolated pair", { timeout: 180_000 }, (t) => {
  const client = process.env["KNOWLEDGE_BIN"];
  const server = process.env["KNOWLEDGE_SERVER_BIN"];
  if (client === undefined || client === "" || server === undefined || server === "") {
    // The same rule the registration row follows: a skip is not a red, so in CI
    // this must be impossible. Locally it is honest.
    if (process.env["CI"] !== undefined && process.env["CI"] !== "") {
      assert.fail(
        "KNOWLEDGE_BIN and KNOWLEDGE_SERVER_BIN must both be set in CI: this row is the only place the " +
          "COLLECT half of the requirement is observed, and without the pair the leg proves registration alone.",
      );
    }
    t.skip(
      "KNOWLEDGE_BIN and KNOWLEDGE_SERVER_BIN are not both set: this row starts an isolated client and " +
        "server pair and drives a real collect through them. Build both and set them to run it locally.",
    );
    return;
  }
  assert.ok(existsSync(client), `KNOWLEDGE_BIN names ${client}, which does not exist`);
  assert.ok(existsSync(server), `KNOWLEDGE_SERVER_BIN names ${server}, which does not exist`);

  const scratch = mkdtempSync(join(tmpdir(), "kn-ts-e2e-"));
  const home = join(scratch, "home");
  const graphs = join(scratch, "graphs");
  const tree = join(scratch, "tree");
  mkdirSync(home);
  mkdirSync(graphs);
  mkdirSync(join(tree, "nested"), { recursive: true });
  writeFileSync(join(tree, "alpha.txt"), "alpha");
  writeFileSync(join(tree, "beta.txt"), "bb");
  writeFileSync(join(tree, "nested", "gamma.txt"), "ggg");

  const graphPort = pickFreePort(15801);
  const mcpPort = pickFreePort(graphPort + 1);
  const env = isolatedEnv(home);
  const children: LoggedChild[] = [];
  const diagnostics = (): string => children.map((c) => c.diagnostic()).join("");

  try {
    children.push(
      spawnLogged("server", scratch, server, ["-port", String(graphPort), "-graph-storage", graphs], env),
    );
    children.push(
      spawnLogged("client", scratch, client, [
        "serve",
        "-port", String(graphPort),
        "-http-port", String(mcpPort),
        "-no-auth",
        // NO LLM PIPELINE. Nothing here asserts a search result, and wiring it
        // needs a provider, and a configured provider is DIALED at startup.
        "-no-llm-pipeline",
        "-no-propagation-runtime",
        "-no-transcript-upload",
        "-no-auto-update",
        "-graph-storage", graphs,
      ], env),
    );

    const mcp = new McpSession(mcpPort, scratch);
    let up = false;
    for (let i = 0; i < 60 && !up; i++) {
      try {
        mcp.open();
        up = true;
      } catch {
        spawnSync("sleep", ["1"]);
      }
    }
    assert.ok(up, `the isolated client's MCP endpoint never came up.${diagnostics()}`);

    // --- R4: THE PAIR MADE NO OUTBOUND CALL ----------------------------------
    // R4.1's own instrument is a static scan of this package's sources for socket
    // shapes, and it cannot see a call made by a process the suite SPAWNED. This
    // is that observation: the client's own log is where an outbound provider
    // dial records itself, and none of these lines may appear.
    //
    // THE CONTROL COMES FIRST, because an absence in an empty buffer is the
    // absence of a reader rather than of a call.
    assert.ok(
      children.every((c) => c.bytesRead > 0),
      `control: both children must have written something for the absences below to be about them. ${diagnostics()}`,
    );
    // EVERY SHAPE HERE OCCURS ONLY ON A COMPLETED OUTBOUND CALL. "precheck
    // failed" is NOT among them and was removed after it fired on a clean run:
    // without a provider the same line reports a purely local config resolution,
    // `resolve "summarizer": config: consumer "summarizer" has no provider`, in 21
    // microseconds. A pattern that cannot tell a local refusal from a vendor round
    // trip is not an instrument for this question. What remains is the log line
    // written immediately BEFORE the dial, and the things only a vendor can say
    // back.
    for (const shape of [
      /pinging LLM provider/i,
      /api\.anthropic\.com/i,
      /request_id/i,
      /http_40\d/i,
      /authentication_error/i,
      /invalid x-api-key/i,
    ]) {
      assert.equal(
        shape.test(children.map((c) => c.stderr).join("\n")),
        false,
        `the pair dialed a third-party API: ${String(shape)} appears in a child's log. The suite makes no ` +
          `network call, and a credential this test supplied is still a credential.${diagnostics()}`,
      );
    }

    // --- R3.1: registration DIALS the provider before it writes anything ------
    const script = join(packageRoot(), "dist", "examples", "sample-collector.js");
    const added = spawnSync(
      client,
      [
        "collector", "add", "--tool", "collect",
        "-e", `HOME=${home}`,
        "-e", "SAMPLE_TOKEN=kn-t37-planted-secret",
        "sample-ts", "--", process.execPath, script,
      ],
      { env, encoding: "utf8" },
    );
    assert.equal(added.status, 0, `collector add failed: ${added.stderr}${added.stdout}${diagnostics()}`);

    // --- R3.3: the collect lands and is readable on its family ----------------
    const collected = mcp.call("collect", {
      type: "sample-ts",
      id: "probe",
      params: { root: tree, depth: 2 },
    });
    assert.equal(collected.isError, false, `the collect failed: ${collected.text}${diagnostics()}`);
    assert.match(collected.text, /nodes 5/, `expected five nodes, got: ${collected.text}`);
    assert.match(collected.text, /edges 4/);

    const walked = mcp.call("traverse", {
      graph: "sample-ts",
      name: "probe",
      start: tree,
      edge_types: ["contains"],
      depth: 3,
    });
    assert.equal(walked.isError, false, walked.text + diagnostics());
    for (const entry of ["alpha.txt", "beta.txt", "gamma.txt"]) {
      assert.match(walked.text, new RegExp(entry), `the family must be readable: ${entry} missing`);
    }

    // The node the collector emitted, read back BY ID, with the metadata it set.
    const node = mcp.call("query", { graph: "sample-ts", name: "probe", id: join(tree, "alpha.txt") });
    assert.equal(node.isError, false, node.text + diagnostics());
    assert.match(node.text, /size_bytes/, `the node's own metadata must survive the round trip: ${node.text}`);

    // --- R3.7: no credential reaches a stored node ----------------------------
    assert.equal(
      /kn-t37-planted-secret|SAMPLE_TOKEN/.test(walked.text + node.text),
      false,
      "the planted secret is in the child's whole environment and must reach no stored node",
    );

    // --- R3.4: a second collect changes nothing a reader can see --------------
    const second = mcp.call("collect", { type: "sample-ts", id: "probe", params: { root: tree, depth: 2 } });
    assert.equal(second.isError, false, second.text);
    const walkedAgain = mcp.call("traverse", {
      graph: "sample-ts",
      name: "probe",
      start: tree,
      edge_types: ["contains"],
      depth: 3,
    });
    // THE MEASUREMENT IS NAMED. What is byte-identical across two collects is the
    // COLLECTOR's encoded result and the READ-PATH rendering; the stored bytes are
    // not, because the write path stamps each collect. This asserts the read path.
    assert.equal(walkedAgain.text, walked.text, "an unchanged tree collected twice must read back identically");

    // --- R3.5: what walk_complete actually decides ----------------------------
    // The deletion pair is the row that matters: an INCOMPLETE collect must not
    // let the server treat a row it did not carry as gone, and a COMPLETE one
    // must. The control is the second half; without it the first proves nothing.
    rmSync(join(tree, "beta.txt"));
    const locked = join(tree, "locked");
    mkdirSync(locked);
    spawnSync("chmod", ["000", locked]);
    const partial = mcp.call("collect", { type: "sample-ts", id: "probe", params: { root: tree, depth: 2 } });
    spawnSync("chmod", ["755", locked]);
    assert.equal(partial.isError, false, partial.text);

    const afterIncomplete = mcp.call("query", { graph: "sample-ts", name: "probe", id: join(tree, "beta.txt") });
    assert.equal(
      afterIncomplete.isError,
      false,
      `a collect whose walk asserted INCOMPLETE must not delete the row it did not carry: ${afterIncomplete.text}`,
    );

    rmSync(locked, { recursive: true, force: true });
    const complete = mcp.call("collect", { type: "sample-ts", id: "probe", params: { root: tree, depth: 2 } });
    assert.equal(complete.isError, false, complete.text);
    const afterComplete = mcp.call("query", { graph: "sample-ts", name: "probe", id: join(tree, "beta.txt") });
    assert.equal(
      afterComplete.isError,
      true,
      "THE CONTROL: a COMPLETE collect that no longer carries the row must let the server treat it as gone, " +
        `or the assertion above is about a deletion phase that never runs. It said: ${afterComplete.text}`,
    );

    // --- R3.6: a throwing walk is an error result, through the client ---------
    const boom = mcp.call("collect", { type: "sample-ts", id: "boom", params: { root: "" } });
    assert.equal(boom.isError, true, `a throwing walk must refuse the collect: ${boom.text}`);
    assert.match(boom.text, /the walk failed/, boom.text);
    assert.match(boom.text, /names the directory to walk/, boom.text);
  } finally {
    // TEARDOWN IS NOT THE VERDICT. The pair writes into the graph directory until
    // it dies, so removing the scratch tree while they are still exiting raced and
    // threw ENOTEMPTY over whatever the assertions had decided. Kill, wait for the
    // exit, then remove with bounded retries; a scratch directory that survives is
    // reported on stderr rather than turned into a failure this row did not find.
    for (const child of children) child.process.kill();
    spawnSync("sleep", ["2"]);
    try {
      rmSync(scratch, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
    } catch (err) {
      process.stderr.write(`endtoend: could not remove ${scratch}: ${String(err)}\n`);
    }
  }
});
