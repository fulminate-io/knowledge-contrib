// sample.test.ts — the README's worked collector: its walk arms, its completeness
// verdict on every outcome of a read (N5), its derived ids (N4), and the two
// credential rows (R3.7, R6.4).

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";

import { SampleCollector } from "../examples/sample-collector.js";
import { ForeignContext } from "../src/context.js";
import { checkToolSchemas } from "../src/contract.js";
import { encodeResult } from "../src/envelope.js";
import { newSpeakerDefinition } from "../src/serve.js";
import { compiled, driveSession, initializeRequest, packageRoot, readText, rpc, runChild } from "./helpers.js";

type Msg = Record<string, any>;

/** A fixture tree the test builds, so every expectation below is independent of it. */
function fixtureTree(): string {
  const root = mkdtempSync(join(tmpdir(), "kn-ts-sample-"));
  writeFileSync(join(root, "alpha.txt"), "alpha");
  writeFileSync(join(root, "beta.txt"), "bb");
  mkdirSync(join(root, "nested"));
  writeFileSync(join(root, "nested", "gamma.txt"), "ggg");
  return root;
}

test("R3.3 the sample walk emits one node per entry and a contains edge for each", () => {
  const root = fixtureTree();
  try {
    const result = new SampleCollector().walk("probe", { root, depth: 2 }, new ForeignContext());
    const out = encodeResult("collect", result);
    // N4: THE EXPECTATION IS COMPUTED FROM THE FIXTURE THIS TEST CREATED, never
    // read back from what the collector emitted.
    // THE ORDER IS ASSERTED, not just the set. readdir's order is filesystem
    // dependent and is not the same twice on every filesystem, so the collector
    // sorts each listing — and a set comparison would pass just as happily
    // without that, leaving the byte-idempotence row below resting on luck.
    assert.deepEqual(out.nodes.map((n) => n["id"] as string), [
      root,
      join(root, "alpha.txt"),
      join(root, "beta.txt"),
      join(root, "nested"),
      join(root, "nested", "gamma.txt"),
    ]);
    assert.deepEqual(out.edges.map((e) => `${e["from_id"] as string}->${e["to_id"] as string}`), [
      `${root}->${join(root, "alpha.txt")}`,
      `${root}->${join(root, "beta.txt")}`,
      `${root}->${join(root, "nested")}`,
      `${join(root, "nested")}->${join(root, "nested", "gamma.txt")}`,
    ]);
    assert.equal(out.walk_complete, true);
    const alpha = out.nodes.find((n) => n["id"] === join(root, "alpha.txt"))!;
    assert.equal(alpha["type"], "file");
    assert.equal(alpha["metadata"]!["size_bytes" as never], "5" as never);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("R3.4 two walks of an unchanged tree produce byte-identical results", () => {
  const root = fixtureTree();
  try {
    const c = new SampleCollector();
    const first = JSON.stringify(encodeResult("collect", c.walk("probe", { root, depth: 2 }, new ForeignContext())));
    const second = JSON.stringify(encodeResult("collect", c.walk("probe", { root, depth: 2 }, new ForeignContext())));
    assert.equal(second, first, "the listing is sorted, so one unchanged input is one result byte for byte");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("N5 a subtree it cannot read makes the WALK incomplete, with the reason naming the path", (t) => {
  if (process.getuid?.() === 0) {
    t.skip("running as root, where a mode-000 directory is still readable and this arm cannot be produced");
    return;
  }
  const root = fixtureTree();
  const locked = join(root, "locked");
  mkdirSync(locked);
  chmodSync(locked, 0o000);
  try {
    const result = new SampleCollector().walk("probe", { root, depth: 2 }, new ForeignContext());
    assert.equal(result.complete.isComplete(), false, "a walk that could not read a subtree is INCOMPLETE");
    assert.match(result.complete.reason(), /locked/);
    const out = encodeResult("collect", result);
    assert.equal(out.walk_complete, false, "and the assertion reaches the wire, which is what disables deletion");
    // THE CONTROL: the readable half still landed, so this is a partial walk
    // reported honestly rather than a walk that gave up.
    assert.ok(out.nodes.some((n) => n["id"] === join(root, "alpha.txt")));
  } finally {
    chmodSync(locked, 0o755);
    rmSync(root, { recursive: true, force: true });
  }
});

test("N5 control: the same tree with nothing locked asserts COMPLETE", () => {
  const root = fixtureTree();
  try {
    const result = new SampleCollector().walk("probe", { root, depth: 2 }, new ForeignContext());
    assert.equal(result.complete.isComplete(), true);
    assert.equal(result.complete.reason(), "");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("N5 a root that does not exist at all makes the walk incomplete rather than empty-and-complete", () => {
  const result = new SampleCollector().walk(
    "probe",
    { root: join(tmpdir(), "kn-ts-sample-does-not-exist-" + String(Date.now())) },
    new ForeignContext(),
  );
  assert.equal(result.complete.isComplete(), false);
  assert.match(result.complete.reason(), /could not read/);
});

test("R3.6 a walk that throws becomes isError with text, driven over the wire", async () => {
  const s = driveSession(newSpeakerDefinition(new SampleCollector()));
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(
    rpc(2, "tools/call", { name: "collect", arguments: { id: "probe", params: { root: "" } } }),
  )) as Msg[];
  assert.equal(reply!["result"]["isError"], true);
  const text = (reply!["result"]["content"] as Msg[])[0]!["text"] as string;
  assert.match(text, /names the directory to walk/);
  // AND IT IS THE COLLECTOR LAYER THAT CAUGHT IT, not the speaker's own
  // last-resort catch: the prefix is what tells an operator which collector and
  // which phase failed, and without it the same text arrives with no subject.
  assert.match(text, /^collect collector: the walk failed: /);
  assert.equal("structuredContent" in reply!["result"], false);
});

test("R3.1 the sample serves when registered by a SYMLINKED path, not only by its real one", async () => {
  // THE DEFECT THIS ROW EXISTS FOR, reproduced against a real client before it
  // was fixed: an entry-point guard comparing `import.meta.url` to
  // "file://" + process.argv[1] is FALSE whenever any prefix of the path is a
  // symlink, because import.meta.url is resolved and argv[1] is whatever the
  // operator typed. The collector then loads, serves nothing, exits 0, and the
  // registration fails with a handshake EOF that names nothing.
  const link = join(mkdtempSync(join(tmpdir(), "kn-ts-link-")), "pkg");
  symlinkSync(packageRoot(), link);
  try {
    const viaLink = join(link, "dist", "examples", "sample-collector.js");
    const run = await runChild(viaLink, [initializeRequest(1, "2025-06-18")], { HOME: tmpdir() });
    assert.equal(run.messages.length, 1, `the collector must serve through a symlinked path: ${run.stderr}`);
    const reply = run.messages[0] as Msg;
    assert.equal(reply["result"]["serverInfo"]["name"], "collect");

    // THE CONTROL, same run: through the REAL path it serves too, so the row is
    // about the spelling rather than about the collector.
    const direct = await runChild(
      join(packageRoot(), "dist", "examples", "sample-collector.js"),
      [initializeRequest(1, "2025-06-18")],
      { HOME: tmpdir() },
    );
    assert.equal(direct.messages.length, 1);
  } finally {
    rmSync(dirname(link), { recursive: true, force: true });
  }
});

test("R3.1 and R4.4 the REAL client registers the sample, which is a dial rather than a transcription", (t) => {
  // `knowledge collector add` DIALS the provider it is about to register: it
  // spawns the command, completes the MCP handshake, lists the tools and runs the
  // schema gate, and writes nothing when any of that fails. So this row is the
  // only one in this suite where the other side of the wire is the client's own
  // code rather than a driver written here.
  //
  // IT SKIPS BY NAME WITHOUT THE BINARY, never silently: a CI leg that stopped
  // building the client would otherwise report a quieter green than it earned.
  const bin = process.env["KNOWLEDGE_BIN"];
  if (bin === undefined || bin === "") {
    // A SKIP IS NOT A RED, and that is the whole point of this branch. `node
    // --test` exits 0 with skipped rows, so a leg whose env line was dropped, or
    // whose client build silently produced nothing, would pass having proven
    // nothing about the only venue where an INDEPENDENT client drives this
    // speaker. In CI that must be impossible; locally the skip is honest.
    if (process.env["CI"] !== undefined && process.env["CI"] !== "") {
      assert.fail(
        "KNOWLEDGE_BIN is unset in CI: the leg must build the knowledge client and pass it here. " +
          "This row and the contract-test target are the only two venues where an independent client " +
          "drives this package's speaker, so a green leg without it proves the protocol against nothing " +
          "but the driver written for it.",
      );
    }
    t.skip(
      "KNOWLEDGE_BIN is unset: this row registers the sample through the REAL client, which the CI leg builds " +
        "and passes here. Set it to a knowledge binary to run the dial locally.",
    );
    return;
  }
  assert.ok(existsSync(bin), `KNOWLEDGE_BIN names ${bin}, which does not exist`);

  const home = mkdtempSync(join(tmpdir(), "kn-ts-reg-"));
  try {
    const script = join(packageRoot(), "dist", "examples", "sample-collector.js");
    // The child's env is the WHOLE environment, exactly as the entry's block is.
    const added = spawnSync(
      bin,
      ["collector", "add", "--tool", "collect", "-e", `HOME=${home}`, "sample-ts", "--", process.execPath, script],
      { env: { HOME: home, PATH: "/usr/bin:/bin" }, encoding: "utf8" },
    );
    assert.equal(
      added.status,
      0,
      `collector add must dial, handshake, list and pass the schema gate; it said: ${added.stderr}${added.stdout}`,
    );

    const written = JSON.parse(readText(join(home, ".knowledge", "collectors.json"))) as Record<string, any>;
    const entry = written["collectors"]["sample-ts"];
    assert.equal(entry["type"], "stdio");
    assert.equal(entry["command"], process.execPath, "the interpreter is the command");
    assert.deepEqual(entry["args"], [script], "and the compiled script is an args element, each its own argv element");
    assert.equal(entry["tool"], "collect");

    // THE SAME-RUN NEGATIVE, through the same instrument: a provider that does
    // not satisfy the contract is REFUSED and nothing is written. Without it a
    // client that wrote every entry unread would pass the row above.
    const refused = spawnSync(
      bin,
      ["collector", "add", "--tool", "collect", "not-a-collector", "--", process.execPath, "-e", "process.exit(0)"],
      { env: { HOME: home, PATH: "/usr/bin:/bin" }, encoding: "utf8" },
    );
    assert.notEqual(refused.status, 0, "a provider that speaks no MCP must be refused at registration");
    const after = JSON.parse(readText(join(home, ".knowledge", "collectors.json"))) as Record<string, any>;
    assert.equal("not-a-collector" in after["collectors"], false, "and nothing may be written for it");
  } finally {
    rmSync(home, { recursive: true, force: true });
  }
});

test("R3.1 the sample's advertised schemas pass the gate the client runs at registration", async () => {
  const s = driveSession(newSpeakerDefinition(new SampleCollector()));
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/list"))) as Msg[];
  const tool = (reply!["result"]["tools"] as Msg[])[0]!;
  assert.equal(tool["name"], "collect");
  assert.doesNotThrow(() => checkToolSchemas("collect", tool["inputSchema"], tool["outputSchema"]));
});

test("R3.7 and R6.4 the sample reads no credential and emits none, with a same-run positive", async () => {
  const root = fixtureTree();
  try {
    // THE KNOWN POSITIVE: the child's whole environment carries a credential-shaped
    // name, so a zero below is a fact about the collector rather than about a
    // variable that was never set.
    const secret = "kn-ts-sample-secret-value";
    const run = await runChild(
      compiled("examples/sample-collector.js"),
      [
        initializeRequest(1, "2025-06-18"),
        rpc(2, "tools/call", { name: "collect", arguments: { id: "probe", params: { root, depth: 2 } } }),
      ],
      { SAMPLE_TOKEN: secret, HOME: root, PATH: "/usr/bin:/bin" },
    );
    // R1.12 FOR THE SAMPLE, which is the artifact the README tells an author to
    // copy. The library half is asserted against the conformance stub; this is the
    // sample half, and it was silent: both rows that spawn the sample read only
    // the PARSED messages, and runChild keeps a non-JSON line in stdoutLines and
    // skips it at the parser, so a stray print was invisible to them.
    //
    // ASSERT OVER THE RAW LINES, not over the parsed messages. A runtime assertion
    // on the child's own stream is the coarsest sufficient carrier: it catches
    // console.log, console.info, console.debug, a bare process.stdout.write and a
    // dependency that logs, all at once, where a text pattern would track
    // spellings.
    assert.ok(run.stdoutLines.length > 0, "control: the sample really wrote to stdout");
    for (const line of run.stdoutLines) {
      let parsed: Msg;
      try {
        parsed = JSON.parse(line) as Msg;
      } catch {
        assert.fail(
          `the sample wrote a non-JSON-RPC line to stdout: ${JSON.stringify(line.slice(0, 120))}. ` +
            `stdout is the protocol stream; a stray print corrupts the framing and reaches an operator ` +
            `as an opaque handshake failure.`,
        );
      }
      assert.equal(parsed["jsonrpc"], "2.0", `stdout line is not a JSON-RPC message: ${line.slice(0, 120)}`);
    }
    assert.equal(
      run.stdoutLines.length,
      run.messages.length,
      "every line on the protocol stream is one of the messages, and no line was skipped at the parser",
    );

    const result = (run.messages.find((m) => (m as Msg)["id"] === 2) as Msg)["result"] as Msg;
    const wire = JSON.stringify(result);
    // A NON-REVERSIBLE PROPERTY: presence, never the value.
    assert.equal(wire.includes(secret), false, "no environment value may reach a stored node");
    assert.equal(wire.includes("SAMPLE_TOKEN"), false, "and no environment NAME either");
    assert.equal(
      wire.includes("env"),
      false,
      "the whole environment is never serialized, which is the shape the contract's own stub refuses",
    );
    // The control that the run produced a real graph, so the absences are not
    // the absence of a result.
    assert.ok((result["structuredContent"]["nodes"] as unknown[]).length >= 4);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("R3.7 every key the sample emits is in the contract's node vocabulary plus metadata", () => {
  const root = fixtureTree();
  try {
    const out = encodeResult(
      "collect",
      new SampleCollector().walk("probe", { root, depth: 2 }, new ForeignContext()),
    );
    const allowed = new Set([
      "id",
      "type",
      "symbol_name",
      "file_path",
      "language",
      "start_line",
      "end_line",
      "content",
      "signature",
      "summary",
      "description",
      "source",
      "status",
      "keywords",
      "is_exported",
      "metadata",
    ]);
    for (const node of out.nodes) {
      for (const key of Object.keys(node)) {
        assert.ok(allowed.has(key), `the sample emitted the node key ${key}, which is not in the vocabulary`);
      }
    }
    const metaKeys = new Set(
      out.nodes.flatMap((n) => Object.keys((n["metadata"] as Record<string, string>) ?? {})),
    );
    assert.deepEqual([...metaKeys], ["size_bytes"], "the sample's metadata is a file size and nothing else");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("R1.8 the sample refuses a collect whose params fail its OWN declared schema, before the walk", async () => {
  const s = driveSession(newSpeakerDefinition(new SampleCollector()));
  await s.send(initializeRequest(1, "2025-06-18"));
  for (const [args, wants] of [
    [{ id: "probe" }, "root"],
    [{ id: "probe", params: {} }, "root"],
    [{ id: "probe", params: { root: 7 } }, "root"],
    [{ id: "probe", params: { root: "/tmp", depth: 0 } }, "depth"],
    [{ id: "probe", params: { root: "/tmp", unknown: 1 } }, "unknown"],
  ] as Array<[Record<string, unknown>, string]>) {
    const [reply] = (await s.send(rpc(9, "tools/call", { name: "collect", arguments: args }))) as Msg[];
    assert.equal(reply!["result"]["isError"], true, `${JSON.stringify(args)} must be refused`);
    assert.ok(
      ((reply!["result"]["content"] as Msg[])[0]!["text"] as string).includes(wants),
      `the refusal must name ${wants}`,
    );
  }
});
