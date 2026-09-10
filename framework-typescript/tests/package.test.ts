// package.test.ts — the rows about the PACKAGE rather than about the code it
// contains: the dependency set, the Node pin, the no-network claim, the publish
// trap, the manifest, the README, and the pins that turn when a sibling change
// lands.
//
// EVERY ROW THAT READS OUTSIDE THIS PACKAGE SAYS WHICH LAYOUT IT IS IN. This
// suite is the manifest's declared test command, so it runs in the published tree
// as well as here; a row whose subject exists only in this repository SKIPS BY
// NAME with the reason printed rather than passing silently in both.

import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";
import { test } from "node:test";

import { Ajv2020 } from "ajv/dist/2020.js";

import { SampleCollector } from "../examples/sample-collector.js";
import { DESCRIBE_TOOL_NAME } from "../src/describe.js";
import { describeContractJSON } from "../src/schema.js";
import { newSpeakerDefinition } from "../src/serve.js";
import { packageRoot, readText, repoRoot } from "./helpers.js";

type Json = Record<string, any>;

const ROOT = packageRoot();
const pkg = (): Json => JSON.parse(readText(join(ROOT, "package.json"))) as Json;

/**
 * The in-repo module prefix, ASSEMBLED rather than written out. A file in this
 * package carrying that string whole would abort the publish: the sync rewrites
 * module paths in Go sources only, and its survivor grep then scans EVERY file.
 * Spelling the guard in pieces is what lets the guard exist inside the thing it
 * guards.
 */
const IN_REPO_MODULE_PREFIX = ["github.com/fulminate-io", "knowledge", "cmd", "collectors"].join("/");
const PUBLISHED_MODULE_PREFIX = ["github.com/fulminate-io", "knowledge-contrib"].join("/");

/** Every file this package ships or commits, except the build output and node_modules. */
function packageFiles(): string[] {
  const out: string[] = [];
  const skip = new Set(["node_modules", "dist", ".git"]);
  const walk = (dir: string): void => {
    for (const entry of readdirSync(dir).sort()) {
      if (skip.has(entry)) continue;
      const path = join(dir, entry);
      if (statSync(path).isDirectory()) walk(path);
      else out.push(path);
    }
  };
  walk(ROOT);
  return out;
}

// --- R6.1: the dependency set --------------------------------------------

test("R6.1 the runtime dependency set is exactly ajv, and nothing may join it unnoticed", () => {
  assert.deepEqual(
    Object.keys(pkg()["dependencies"] as Json),
    ["ajv"],
    "the speaker is hand-rolled and ajv is the only runtime dependency; an added one is a lockfile this repository publishes and scans",
  );
  assert.deepEqual(
    Object.keys(pkg()["devDependencies"] as Json).sort(),
    ["@types/node", "typescript"],
    "the compiler and its ambient types are the whole dev set; the test runner is node:test and pulls nothing",
  );
  for (const [name, range] of Object.entries({ ...pkg()["dependencies"], ...pkg()["devDependencies"] } as Json)) {
    assert.match(
      range as string,
      /^\d+\.\d+\.\d+$/,
      `${name} must be pinned to an exact version, not a range: a published lockfile is only reproducible if it is`,
    );
  }
});

test("R6.1 the package is npm-private, and carries no path that could publish it", () => {
  // THE DECISION: this port is a library. No install script, no npm publish, no
  // release archive. `private` is npm's OWN refusal — with it, `npm publish` in
  // this directory fails outright — and it is the only one of the guards below
  // that a person running the command by hand actually meets. The others assert
  // that no path was quietly added back.
  assert.equal(pkg()["private"], true, "npm's own refusal to publish this package");
  assert.equal("publishConfig" in pkg(), false, "no registry or access is configured for it");
  for (const name of Object.keys(pkg()["scripts"] as Json)) {
    assert.equal(
      name === "prepare" || name.startsWith("prepublish"),
      false,
      `the script ${name} runs on npm publish and pack; this package declares no packaging lifecycle`,
    );
  }
  // THE CONTROL: the scripts really were read, so the loop above is not vacuous.
  assert.deepEqual(Object.keys(pkg()["scripts"] as Json).sort(), ["build", "test"]);
});

test("R6.1 no MCP SDK is declared, on either side of the manifest", () => {
  const all = { ...(pkg()["dependencies"] as Json), ...(pkg()["devDependencies"] as Json) };
  for (const name of Object.keys(all)) {
    assert.equal(
      name.startsWith("@modelcontextprotocol/"),
      false,
      "this package speaks MCP itself; the SDK validates neither call arguments nor results and costs 86 further packages",
    );
  }
});

test("R6.1 the lockfile is committed and describes exactly the declared set", () => {
  const lockPath = join(ROOT, "package-lock.json");
  assert.ok(existsSync(lockPath), "npm ci needs a committed lockfile; npm install is not the install step");
  const lock = JSON.parse(readText(lockPath)) as Json;
  assert.equal(lock["lockfileVersion"] >= 3, true);
  const entries = Object.keys(lock["packages"] as Json).filter((k) => k !== "");
  const names = entries.map((k) => k.replace(/^node_modules\//, "")).sort();
  assert.deepEqual(
    names,
    ["@types/node", "ajv", "fast-deep-equal", "fast-uri", "json-schema-traverse", "require-from-string", "typescript", "undici-types"],
    "the resolved set is the declared one plus ajv's four transitive packages and the compiler's ambient types",
  );
  assert.equal(entries.length, 8, "eight packages, against the 116 the MCP SDK route measured");
});

// --- R4.1, R4.2: no network, no runner dependency ------------------------

test("R4.1 no source or test file in this package opens a socket", () => {
  // THE PATTERNS ARE ASSEMBLED FROM PIECES rather than written out, for the same
  // reason the publish guard above is: this file is one of the files the scan
  // reads, and a pattern spelled whole here would match itself and red every run.
  const sockets = ["net", "http", "https", "dgram", "tls", "dns"].join("|");
  const networkShapes = [
    new RegExp(`\\bfrom\\s+["']node:(${sockets})["']`),
    new RegExp(`\\brequire\\(["']node:(${sockets})["']\\)`),
    new RegExp(`\\b${"fet" + "ch"}\\s*\\(`),
    new RegExp(`\\bnew\\s+${"Web" + "Socket"}\\b`),
    new RegExp(`\\b${"XMLHttp" + "Request"}\\b`),
  ];
  const scanned: string[] = [];
  for (const path of packageFiles()) {
    if (!/\.(ts|mts|cts)$/.test(path)) continue;
    const text = readText(path);
    scanned.push(path);
    for (const shape of networkShapes) {
      assert.equal(
        shape.test(text),
        false,
        `${relative(ROOT, path)} matches ${String(shape)}; the suite makes no network call and neither does the library`,
      );
    }
  }
  // THE CONTROL, in two halves, because a zero has two innocent explanations: a
  // walk that read no file, and an instrument that matches nothing.
  assert.ok(scanned.length >= 10, `control: the scan read ${scanned.length} TypeScript files`);
  // The known positives are assembled too, for the same self-match reason.
  const nodeModule = (m: string): string => `no${"de"}:${m}`;
  for (const [i, planted] of [
    `import { createServer } from "${nodeModule("http")}";`,
    `const s = require("${nodeModule("net")}");`,
    "await " + "fet" + "ch(url);",
    "new " + "Web" + "Socket(url);",
    "new " + "XMLHttp" + "Request();",
  ].entries()) {
    assert.equal(networkShapes[i]!.test(planted), true, `control: shape ${i} matches its own known positive`);
  }
});

test("R4.2 the test runner is node:test and needs no package", () => {
  for (const path of packageFiles()) {
    if (!/\.test\.ts$/.test(path)) continue;
    assert.match(
      readText(path),
      /from "node:test"/,
      `${relative(ROOT, path)} must use the built-in runner; a runner with a dependency tree is cost with no matching benefit`,
    );
  }
  assert.match(pkg()["scripts"]["test"] as string, /node --test/);
});

// --- R4.3: the Node pin ---------------------------------------------------

test("R4.3 the Node version is pinned in ONE file, and every other statement of it agrees by test", () => {
  const nvmrc = readText(join(ROOT, ".nvmrc")).trim();
  assert.match(nvmrc, /^\d+$/, ".nvmrc carries the major and nothing else, so setup-node resolves the latest patch");
  const major = Number(nvmrc);
  assert.ok(major >= 22, "node:test and node:readline are long stable on the LTS line this pins");

  // package.json's `engines` is npm's own carrier and cannot read a file, so the
  // two spellings are held together HERE rather than left to drift.
  assert.equal(
    pkg()["engines"]["node"],
    `>=${nvmrc}`,
    "engines.node must state the same floor .nvmrc pins",
  );
});

test("R4.3 the CI leg reads the pin file rather than restating the number", (t) => {
  const root = repoRoot();
  if (root === undefined) {
    t.skip("the workflow lives in this repository only; running from the published layout");
    return;
  }
  const ci = readText(join(root, ".github", "workflows", "ci.yml"));
  const rel = "cmd/collectors/framework-typescript/.nvmrc";
  assert.ok(
    ci.includes(`node-version-file: ${rel}`),
    `the port's leg must set node-version-file: ${rel}; a literal version in the workflow is the second spelling this row forbids`,
  );
  assert.equal(
    /node-version:\s*['"]?\d+/.test(ci),
    false,
    "no job may state a Node version as a literal",
  );
});

// --- R6.5(a): the publish trap -------------------------------------------

/**
 * Every file at or under `dir`, as raw bytes, excluding only the installed
 * dependency tree. BUILD OUTPUT IS INCLUDED DELIBERATELY: the guard below spells
 * its needle in pieces so the file carrying the guard is not itself a hit, and a
 * compiler is free to fold `"a" + "b"` back into one literal when it emits. If it
 * ever does, the folded literal lands in the build output and nowhere else, so a
 * scan that read only the sources would report a clean tree while the published
 * one carried the needle. Bytes rather than a decoded string, because the
 * survivor grep the publish runs reads bytes.
 */
function scanForNeedle(dir: string, needle: string): string[] {
  const hits: string[] = [];
  const bytes = Buffer.from(needle, "utf8");
  const skip = new Set(["node_modules", ".git"]);
  const walk = (d: string): void => {
    for (const entry of readdirSync(d).sort()) {
      if (skip.has(entry)) continue;
      const path = join(d, entry);
      if (statSync(path).isDirectory()) walk(path);
      else if (readFileSync(path).includes(bytes)) hits.push(path);
    }
  };
  walk(dir);
  return hits;
}

test("R6.5 no file this package ships names the IN-REPO module path, build output included", () => {
  const hits = scanForNeedle(ROOT, IN_REPO_MODULE_PREFIX);
  assert.deepEqual(
    hits.map((p) => relative(ROOT, p)),
    [],
    "the publish rewrites Go sources only and its survivor grep then scans EVERY file, so any hit here aborts the sync at exit 1",
  );

  // THE CONTROL IS A PLANTED FOLDED LITERAL, READ BACK THROUGH THE SAME WALK,
  // because a zero has two innocent explanations: a walk that read nothing, and a
  // reader that cannot see the needle. Its SHAPE is the hazard's own: the guard
  // above spells its needle in pieces so this file is not itself a hit, and a
  // compiler is free to fold `"a" + "b"` back into one literal when it emits — a
  // sibling port's interpreter did exactly that into its bytecode, and only the
  // census over the built tree caught it. So the plant is a single folded string
  // literal in the build output, which is precisely what a folding compiler would
  // leave behind, rather than a comment that merely contains the characters.
  //
  // It is planted under the build-output directory, which git ignores and the
  // build rewrites, so a leftover can never be committed or staged from a clean
  // checkout.
  const built = join(ROOT, "dist");
  assert.ok(existsSync(built), "control: the build output exists, so the scan really covered it");
  const planted = join(built, "kn-planted-needle-control.js");
  try {
    writeFileSync(planted, `export const folded = "${IN_REPO_MODULE_PREFIX}/framework-typescript";\n`);
    const found = scanForNeedle(ROOT, IN_REPO_MODULE_PREFIX).map((p) => relative(ROOT, p));
    assert.deepEqual(
      found,
      [relative(ROOT, planted)],
      "control: the same walk finds a planted needle in the build output, and finds exactly one",
    );
  } finally {
    rmSync(planted, { force: true });
  }

  // And the walk really reached the files a reader would ask about.
  const names = new Set<string>();
  const collect = (d: string): void => {
    for (const e of readdirSync(d)) {
      if (e === "node_modules" || e === ".git") continue;
      const p = join(d, e);
      if (statSync(p).isDirectory()) collect(p);
      else names.add(relative(ROOT, p));
    }
  };
  collect(ROOT);
  for (const expected of ["package-lock.json", "README.md", manifestName()]) {
    assert.ok(names.has(expected), `control: ${expected} was in the scanned set`);
  }
  assert.ok([...names].some((n) => n.startsWith("dist/")), "control: build output was in the scanned set");
});

test("R6.5 the build output and the installed tree are named in the ignore file, so neither is ever committed", () => {
  // WHAT THIS ROW CAN AND CANNOT REACH, said plainly. The publish copies each
  // top-level directory of the collectors tree with a whole-directory copy that
  // consults no ignore file, so an installed node_modules left in place on a
  // developer's machine IS staged by a local sync run. What the ignore file
  // decides is what reaches the COMMIT, and that is the half this package owns:
  // nothing untracked can ride the publish out of a fresh checkout, which is the
  // tree the publish job stages from. The other half is a property of the sync
  // script and is recorded as a finding rather than asserted here.
  const ignored = readText(join(ROOT, ".gitignore"));
  for (const dir of ["node_modules", "dist"]) {
    assert.match(
      ignored,
      new RegExp(`^${dir}/$`, "m"),
      `.gitignore must name ${dir}, or an installed or built tree becomes committable`,
    );
  }
  // And neither is in the file set this package ships: `files` in package.json
  // names what an npm tarball would carry, and tests and node_modules are out.
  const files = pkg()["files"] as string[];
  for (const entry of files) {
    assert.equal(entry.startsWith("node_modules"), false);
  }
  assert.ok(files.includes("contract"), "control: the file list is the real one and names the contract copies");
});

test("R6.5 a file that names this package's path names the PUBLISHED one", () => {
  const readme = readText(join(ROOT, "README.md"));
  if (readme.includes("github.com/fulminate-io")) {
    assert.ok(
      readme.includes(PUBLISHED_MODULE_PREFIX),
      "the README's only absolute path is the published repository's",
    );
  }
  const manifestPath = join(ROOT, "package.json");
  const text = readText(manifestPath);
  assert.equal(text.includes(IN_REPO_MODULE_PREFIX), false);
});

// --- R7: the manifest ------------------------------------------------------

// THE SPELLING IS NOT THIS PACKAGE'S TO CHOOSE, and it is not the first port's
// either. It comes from scripts/testdata/collector-port-contract.tsv, the ONE
// declaration the four census readers share.
//
// SO NO ROW BELOW WRITES THE NAME DOWN. The package's manifest is DISCOVERED by
// its shape, and the declaration is READ, and the pin compares the two. A literal
// here would be a fifth spelling of the thing whose fifth spelling is the defect:
// it would red on a rename of the package's file and stay silent on a rename of
// the declaration, which is the direction that makes every port invisible to all
// four readers at once.

/** Parses key<TAB>value pairs the way all four readers do. First occurrence wins. */
function tabPairs(text: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const line of text.split("\n")) {
    if (line.trim() === "" || line.startsWith("#")) continue;
    const tab = line.indexOf("\t");
    if (tab <= 0) continue;
    // FIRST WINS, exactly as every reader's `awk ... | head -1` does, so a
    // duplicated key cannot mean one thing here and another there.
    if (!out.has(line.slice(0, tab))) out.set(line.slice(0, tab), line.slice(tab + 1));
  }
  return out;
}

/**
 * Every top-level file in this package that IS a port manifest by shape: a
 * key<TAB>value document declaring a `language`. Discovery rather than a name,
 * so a rename on either side of the contract is visible instead of assumed.
 */
function portManifestCandidates(): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(ROOT).sort()) {
    const path = join(ROOT, entry);
    if (statSync(path).isDirectory()) continue;
    let text: string;
    try {
      text = readText(path);
    } catch {
      continue; // a file this process cannot decode is not a manifest
    }
    if (tabPairs(text).has("language")) out.push(entry);
  }
  return out;
}

/** The name of the manifest this package actually carries, discovered by shape. */
function manifestName(): string {
  const found = portManifestCandidates();
  assert.equal(
    found.length,
    1,
    `this package must carry exactly one port manifest at its root; found [${found.join(", ")}]`,
  );
  return found[0]!;
}

/** The manifest, parsed as its four readers parse it. */
function manifest(): Map<string, string> {
  return tabPairs(readText(join(ROOT, manifestName())));
}

test("R7.1 the package carries the port manifest, with a build, a test, a language and a CI leg", () => {
  const m = manifest();
  assert.equal(m.get("language"), "typescript");
  assert.ok((m.get("build") ?? "").length > 0);
  assert.ok((m.get("test") ?? "").length > 0);
  assert.ok((m.get("ci_leg") ?? "").length > 0);
  // The declared commands are the ones this package really runs.
  assert.match(m.get("build")!, /npm ci/, "the install step is npm ci against the committed lockfile");
  assert.match(m.get("test")!, /npm/);
  assert.equal(m.get("ci_leg"), "framework-typescript-tests");
});

test("R7.1 the package carries NO go.mod: a directory in two classes reds the membership census", () => {
  assert.equal(existsSync(join(ROOT, "go.mod")), false);
  assert.equal(existsSync(join(ROOT, "go.sum")), false);
});

test("R7.1 the manifest's name and key set are the ones the port contract declares", (t) => {
  const carried = manifestName();
  const root = repoRoot();
  if (root === undefined) {
    t.skip(
      `the port contract declaration lives in this repository only; running from the published layout. ` +
        `This package carries ${carried}.`,
    );
    return;
  }
  const contract = join(root, "scripts", "testdata", "collector-port-contract.tsv");
  if (!existsSync(contract)) {
    // PENDING PIN, and the ONLY condition under which this row skips. The one
    // declaration four readers in two languages share has not landed; until it
    // does there is nothing to agree with.
    t.skip(
      `${relative(root, contract)} does not exist at this tree: the change that adds the shared non-Go manifest ` +
        `declaration has not landed. This package carries ${carried}, read from that change in flight. When the ` +
        `declaration lands this row ASSERTS, and reds on a rename in EITHER direction.`,
    );
    return;
  }

  // Read BOTH halves off the declaration; write neither down.
  const declared = readText(contract)
    .split("\n")
    .filter((l) => l.includes("\t") && !l.startsWith("#"))
    .map((l) => [l.slice(0, l.indexOf("\t")), l.slice(l.indexOf("\t") + 1)] as const);
  const declaredName = declared.find(([k]) => k === "manifest")?.[1];
  const declaredKeys = declared.filter(([k]) => k === "manifest-key").map(([, v]) => v);

  assert.ok(declaredName !== undefined, `${relative(root, contract)} declares no manifest name`);
  assert.ok(declaredKeys.length > 0, `${relative(root, contract)} declares no manifest keys`);

  assert.equal(
    carried,
    declaredName,
    `the manifest's name comes from the one file that declares it. A disagreement here IS the two-spellings ` +
      `defect: this package carries ${carried} and the declaration names ${String(declaredName)}, so on the ` +
      `losing spelling every port is invisible to all four readers at once.`,
  );

  const m = manifest();
  for (const key of declaredKeys) {
    assert.ok(m.has(key), `the manifest must declare ${key}, which the contract requires`);
  }
  assert.deepEqual(
    [...m.keys()].sort(),
    [...declaredKeys].sort(),
    "and no key beyond them: a key no reader reads is a declaration that does nothing, and the census names it as one",
  );
});

test("R7.1 the manifest's declared CI leg is a job in the workflow", (t) => {
  const root = repoRoot();
  if (root === undefined) {
    t.skip("the workflow lives in this repository only; running from the published layout");
    return;
  }
  const ci = readText(join(root, ".github", "workflows", "ci.yml"));
  const leg = manifest().get("ci_leg")!;
  assert.ok(
    ci.includes(`${leg}:`),
    `the manifest declares the CI leg ${leg} and the workflow must name it; a leg named in one and not the other ` +
      `is exactly the drift the membership census exists to catch`,
  );
});

test("R7.5 the non-Go census arm carries a floor of one once a port exists", (t) => {
  const root = repoRoot();
  if (root === undefined) {
    t.skip("the census scripts live in this repository only; running from the published layout");
    return;
  }
  const census = join(root, "scripts", "collectors-standalone-census.sh");
  const text = readText(census);
  if (!text.includes("port_legs")) {
    t.skip(
      "scripts/collectors-standalone-census.sh carries no non-Go port arm at this tree: the port-infrastructure " +
        "change that adds it, and lands with `ports=0 (arm idle)` and no numeric floor, has not landed. The floor " +
        "of one belongs to whichever port lands first and is owed by this change if it is the first.",
    );
    return;
  }
  assert.equal(
    text.includes("arm idle"),
    false,
    "a port now exists, so the arm's idle banner must be gone and a floor of one must stand in its place: " +
      "a class-(b) loop that ran nothing would otherwise print the same green line as a clean tree",
  );
});

// --- R5: the README --------------------------------------------------------

const readme = (): string => readText(join(ROOT, "README.md"));

test("R5.1 the README's worked registration entry decodes into the loader's own shape", () => {
  const blocks = [...readme().matchAll(/```jsonc?\n([\s\S]*?)```/g)].map((m) => m[1]!);
  const entries = blocks
    .map((b) => {
      try {
        return JSON.parse(b) as Json;
      } catch {
        return undefined;
      }
    })
    .filter((v): v is Json => v !== undefined && "collectors" in v);
  assert.equal(entries.length >= 1, true, "the README carries at least one whole collectors.json block");
  for (const doc of entries) {
    for (const [name, entry] of Object.entries(doc["collectors"] as Json)) {
      const e = entry as Json;
      assert.equal(e["type"], "stdio", `${name}: this package serves stdio`);
      assert.equal(typeof e["command"], "string");
      assert.ok(Array.isArray(e["args"]), `${name}: args is an array, each element its own argv element`);
      assert.equal(typeof e["tool"], "string");
      for (const key of Object.keys(e)) {
        assert.ok(
          ["type", "command", "args", "env", "tool", "behavior", "node_types"].includes(key),
          `${name}: the loader refuses the unknown entry key ${key}`,
        );
      }
      for (const [k, v] of Object.entries((e["env"] ?? {}) as Json)) {
        assert.equal(typeof v, "string", `${name}: env values are strings`);
        assert.equal(k.includes("="), false, `${name}: an env key containing = is refused`);
      }
    }
  }
});

test("R5.2 the README carries no real value", () => {
  const text = readme();
  for (const shape of [
    /\bAKIA[0-9A-Z]{16}\b/,
    /\bghp_[A-Za-z0-9]{20,}\b/,
    /\bxox[baprs]-[A-Za-z0-9-]{10,}\b/,
    /-----BEGIN [A-Z ]*PRIVATE KEY-----/,
    /\/Users\/[a-z]+\//,
    /\/home\/[a-z]+\//,
  ]) {
    assert.equal(shape.test(text), false, `the README matches ${String(shape)}`);
  }
  // The control: the instrument does fire on a planted positive.
  assert.equal(/\/Users\/[a-z]+\//.test("/Users/someone/code"), true);
});

test("R5.3 the README states the absolute path as a RECOMMENDATION with its mechanism, and never as a requirement", () => {
  const text = readme();
  // The mechanism is what makes the recommendation honest, so it must be there.
  assert.match(text, /LookPath|daemon's `?PATH`?/i, "the README must state WHY, not just what");
  assert.match(text, /collector add/, "and that registration proves nothing about the daemon's PATH");
  // And the claim it must NOT make.
  for (const shape of [
    /client requires an absolute path/i,
    /must be an absolute path/i,
    /absolute path is required/i,
    /requires the absolute path/i,
  ]) {
    assert.equal(
      shape.test(text),
      false,
      `the README asserts ${String(shape)}; the client requires no absolute path — it stores the command verbatim ` +
        `and resolves it with one lookup in the process serving the collect`,
    );
  }
});

test("R5.4 the README names the interpreter-plus-script registration shape", () => {
  assert.match(readme(), /collector add[\s\S]{0,400}--[\s\S]{0,120}node/i);
  assert.match(readme(), /dist\/examples\/sample-collector\.js/);
});

test("R5 the README's vocabulary heading states the TOTALS and names how many are optional", () => {
  // The heading used to call 16 and 9 the OPTIONAL vocabulary, which the
  // paragraph under it contradicts in its own words: a node "carries id and type
  // and may carry" fourteen more. This row is what keeps the heading and the
  // paragraph one statement.
  const text = readme();
  const heading = text.split("\n").find((l) => l.startsWith("### ") && l.includes("16 node fields"));
  assert.ok(heading !== undefined, "the vocabulary heading must name the node-field count");
  assert.match(heading, /9 edge fields/);
  assert.match(heading, /14 and 6 are optional/, "and must say how many of them are optional");
  assert.equal(
    /optional vocabulary is 16/.test(text),
    false,
    "16 and 9 are the TOTAL vocabularies; two of each are required",
  );
});

test("R5.5 the README uses no absolute in-repo module path", () => {
  assert.equal(readme().includes(IN_REPO_MODULE_PREFIX), false);
});

test("R5 the README names the build, the test command and the stdout rule", () => {
  const text = readme();
  assert.match(text, /npm ci/);
  assert.match(text, /npm run build/);
  assert.match(text, /npm test/);
  assert.match(text, /stdout/i);
});

// --- the LICENSE -----------------------------------------------------------

test("R7 the package carries the Apache-2.0 text, byte-identical to the one the published root carries", (t) => {
  const mine = join(ROOT, "LICENSE");
  assert.ok(existsSync(mine), "decision: knowledge-contrib is Apache-2.0 and every published directory carries the text");
  assert.match(readText(mine), /Apache License\n\s+Version 2\.0, January 2004/);
  assert.equal(pkg()["license"], "Apache-2.0");

  const root = repoRoot();
  if (root === undefined) {
    t.skip("the contrib LICENSE source lives in this repository only; running from the published layout");
    return;
  }
  const source = join(root, "cmd", "collectors", "contrib", "LICENSE");
  if (!existsSync(source)) {
    t.skip(
      `${relative(root, source)} does not exist at this tree: the change that authors the contrib LICENSE and the ` +
        `framework README has not landed. This package's copy was taken from that change in flight; when it lands, ` +
        `this row asserts the two are byte-identical.`,
    );
    return;
  }
  assert.ok(
    readFileSync(mine).equals(readFileSync(source)),
    "the package's LICENSE is a byte copy of the text the published root carries",
  );
});

// --- R1.13: the describe tool ---------------------------------------------

test("R1.13 the required describe tool is served, against the landed schema", async () => {
  // THE PIN TURNED. It was an assert.fail keyed on a third schema appearing in
  // the client's contract directory, written before the describe tool's shape was
  // settled and saying so; the shape landed, so this is now the assertion it
  // named: a listing carries the tool and its result validates.
  const definition = newSpeakerDefinition(new SampleCollector());
  const served = definition.tools.map((tool) => tool.name);
  assert.ok(
    served.includes(DESCRIBE_TOOL_NAME),
    `the listing carries ${served.join(", ")} and the describe tool is REQUIRED: a provider without it is refused ` +
      `by \`knowledge collector add\` by name and nothing is written`,
  );

  // ITS ADVERTISED OUTPUT SCHEMA IS THE CONTRACT FILE VERBATIM, which is the
  // property the client's registration gate compares.
  const advertised = definition.tools.find((tool) => tool.name === DESCRIBE_TOOL_NAME);
  assert.ok(advertised !== undefined);
  assert.deepEqual(advertised.outputSchema, JSON.parse(describeContractJSON()));

  const result = await definition.call(DESCRIBE_TOOL_NAME, {});
  assert.notEqual(result.isError, true, JSON.stringify(result));
  const document = result.structuredContent;
  assert.ok(document !== undefined, "the describe tool returned no structured content");

  const ajv = new Ajv2020({ allErrors: true, strict: false });
  const validate = ajv.compile(JSON.parse(describeContractJSON()) as object);
  assert.ok(validate(document), JSON.stringify(validate.errors));

  // AND IT SAYS SOMETHING. A declaration that validates while declaring an empty
  // vocabulary is a collector whose every node is refused at ingest, so the
  // schema alone is not the assertion.
  const declared = document as Record<string, unknown>;
  assert.ok((declared["node_types"] as string[]).length > 0, "the sample declares an empty node vocabulary");
  assert.equal(typeof (declared["behavior"] as Record<string, unknown>)["syncable"], "boolean");
});
