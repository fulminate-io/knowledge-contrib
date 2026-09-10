// helpers.ts — the fixtures every test file in this package shares.
//
// THE PACKAGE ROOT IS FOUND BY WALKING UP TO THE package.json rather than by
// counting `..` segments from import.meta.url. The suite runs from dist/tests
// after a build and the sources live in tests/, so a counted path is right in one
// layout and wrong in the other; the walk is right in both, and in the published
// layout too.

import { spawn } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { createInterface } from "node:readline";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import type { SpeakerDefinition, SpeakerIO } from "../src/speaker.js";
import { Session } from "../src/speaker.js";

/** The absolute path of this package's root directory. */
export function packageRoot(): string {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (;;) {
    if (existsSync(join(dir, "package.json"))) return dir;
    const parent = dirname(dir);
    if (parent === dir) throw new Error("helpers: no package.json above " + import.meta.url);
    dir = parent;
  }
}

/**
 * The absolute path of this repository's root, or undefined when this package is
 * running from the PUBLISHED layout, where the repository does not exist.
 *
 * A published tree carries `framework-typescript/` beside `framework/` and no
 * `cmd/` at all, so the marker below is what tells the two apart. Every test that
 * reads a file outside this package uses it and says which layout it is in rather
 * than passing silently in both.
 */
export function repoRoot(): string | undefined {
  const candidate = resolve(packageRoot(), "..", "..", "..");
  return existsSync(join(candidate, "scripts", "sync-to-contrib.sh")) ? candidate : undefined;
}

/**
 * The Go framework's directory, which sits beside this package in BOTH layouts —
 * `cmd/collectors/framework` here, `framework/` in the published tree — so a
 * comparison against it needs no repository and no symlink.
 */
export function goFrameworkDir(): string {
  return resolve(packageRoot(), "..", "framework");
}

/** Reads a file as a string, with no trimming or normalization. */
export function readText(path: string): string {
  return readFileSync(path, "utf8");
}

/** Every outbound message a driven session wrote, parsed. */
export interface DrivenSession {
  session: Session;
  written: unknown[];
  /** Feeds one line in and returns every message written while handling it. */
  send(line: string): Promise<unknown[]>;
  /** The raw lines written, before parsing — what a framing assertion reads. */
  rawLines: string[];
}

/**
 * Drives a speaker definition IN PROCESS, with no child and no pipes. It is how
 * the protocol arms are asserted directly: a test that could only observe the
 * speaker through a client would be asserting the client's behaviour too.
 */
export function driveSession(def: SpeakerDefinition): DrivenSession {
  const rawLines: string[] = [];
  const written: unknown[] = [];
  const io: SpeakerIO = {
    write(line: string) {
      rawLines.push(line);
    },
  };
  const session = new Session(def, io);
  return {
    session,
    written,
    rawLines,
    async send(line: string): Promise<unknown[]> {
      const before = rawLines.length;
      await session.handleLine(line);
      const fresh = rawLines.slice(before);
      const parsed = fresh.map((l) => JSON.parse(l) as unknown);
      written.push(...parsed);
      return parsed;
    },
  };
}

/** One JSON-RPC request, as a line. */
export function rpc(id: number | string | null, method: string, params?: unknown): string {
  const msg: Record<string, unknown> = { jsonrpc: "2.0", method };
  if (id !== null) msg["id"] = id;
  if (params !== undefined) msg["params"] = params;
  return JSON.stringify(msg);
}

/** The initialize request the client sends, with the revision it asks for. */
export function initializeRequest(id: number, protocolVersion: string): string {
  return rpc(id, "initialize", {
    protocolVersion,
    capabilities: {},
    clientInfo: { name: "framework-typescript-tests", version: "v1" },
  });
}

/** What a child collector process answered, and what it wrote where. */
export interface ChildRun {
  /** Every parsed JSON-RPC message the child wrote to stdout. */
  messages: unknown[];
  /** Every raw stdout line, so a framing assertion can see a non-JSON one. */
  stdoutLines: string[];
  stderr: string;
  code: number | null;
  signal: NodeJS.Signals | null;
}

/**
 * Runs a compiled collector or conformance stub as a REAL CHILD PROCESS over real
 * pipes, feeds it the given lines, and collects what it wrote.
 *
 * `env` is the child's WHOLE environment, exactly as the client's config-entry
 * env block is: nothing of this process's environment is inherited, so a test
 * asserting an absence is asserting one this harness really produced.
 */
export function runChild(
  scriptPath: string,
  lines: string[],
  env: Record<string, string> = {},
  options: { closeStdin?: boolean } = {},
): Promise<ChildRun> {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(process.execPath, [scriptPath], {
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const messages: unknown[] = [];
    const stdoutLines: string[] = [];
    let stderr = "";
    const rl = createInterface({ input: child.stdout });
    rl.on("line", (line) => {
      stdoutLines.push(line);
      try {
        messages.push(JSON.parse(line) as unknown);
      } catch {
        // A line that is not JSON is kept in stdoutLines and NOT parsed: the
        // framing assertion is what reads it, and swallowing it here would hide
        // exactly the defect that assertion exists for.
      }
    });
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", rejectPromise);
    child.on("close", (code, signal) => {
      resolvePromise({ messages, stdoutLines, stderr, code, signal });
    });
    for (const line of lines) child.stdin.write(line + "\n");
    if (options.closeStdin !== false) child.stdin.end();
  });
}

/** The compiled path of a source file under this package, after the build. */
export function compiled(relative: string): string {
  return join(packageRoot(), "dist", relative.replace(/\.ts$/, ".js"));
}
