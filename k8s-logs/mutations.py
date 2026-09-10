#!/usr/bin/env python3
"""Kill pass: for each declaration this module adds, remove or invert it and
require a NAMED test to go red.

WHY IT IS CHECKED IN. A test suite that is green proves the code passes the
tests; it does not prove the tests observe the code. This harness is the second
half: it deletes or inverts each guard, control and derived-value rule in turn
and requires a test named for it to fail. A mutation that leaves the suite green
names a declaration nothing observes, which is a defect in the tests rather than
in the code.

HOW TO RUN IT:

    python3 mutations.py            # every mutation
    python3 mutations.py "chunk id" # only mutations whose label contains that

CI DOES NOT RUN IT. It rewrites source files in place (restoring each one in a
finally block), so it is a developer instrument, not a gate.

ITS TWO KNOWN POSITIVES, because a harness that silently measures nothing is
worse than none:

  SETUP-ERROR   the mutation's anchor no longer occurs exactly once in its file,
                so the edit was never applied. Anchors are exact source text and
                they ROT the moment the line is edited; this is the loud failure
                that says so, rather than a silent skip that would read as a
                passing mutation.
  NO-TEST-RAN   the run produced no `=== RUN` line, so either the test pattern
                selected nothing — `go test -run` exits 0 on an empty selection,
                which would read as a green mutation — or the mutation broke the
                build, which is a red but a weaker and less informative one than
                a failing assertion.
"""
import subprocess, sys, os

ROOT = os.path.dirname(os.path.abspath(__file__))

# (label, relative path, old, new, package, test regex)
M = [
 ("template id is truncated", "internal/logpipe/drain.go",
  'return fmt.Sprintf("%x", h[:16])', 'return fmt.Sprintf("%x", h[:])',
  "./internal/logpipe/", "TestTemplateIDIsTruncatedAndBare"),

 ("template id recomputed on broaden", "internal/logpipe/drain.go",
  "\ttpl.ID = TemplateID(pattern)\n", "",
  "./internal/logpipe/", "TestTemplateIDMovesWhenThePatternBroadens"),

 ("template alias recomputed on broaden", "internal/logpipe/drain.go",
  "\ttpl.Alias = TemplateAliasFor(tpl)\n\tif vars != nil", "\tif vars != nil",
  "./internal/logpipe/", "TestTemplateAliasIsRecomputedAfterAMerge"),

 ("unset severity ranks below TRACE", "internal/logpipe/severity.go",
  "\tif sev == \"\" {\n\t\treturn -1\n\t}", "\tif sev == \"\" {\n\t\treturn 0\n\t}",
  "./internal/logpipe/", "TestSeverityRisesToTheMostSevereMember"),

 ("severity comes from the body", "internal/logpipe/severity.go",
  "\tif detected := DetectEmbeddedSeverity(message); detected != \"\" {\n\t\treturn detected\n\t}\n\treturn SeverityInfo",
  "\treturn SeverityInfo",
  "./internal/k8slogs/", "TestSeverityComesFromTheBodyNotTheStream"),

 ("fingerprint hashes only low-card labels", "internal/logpipe/stream.go",
  "Fingerprint:    FingerprintLabels(lowCard),", "Fingerprint:    FingerprintLabels(labels),",
  "./internal/logpipe/", "TestFingerprintUsesOnlyTheLowCardinalityLabels"),

 ("stream order is sorted", "internal/logpipe/stream.go",
  "\tsort.Strings(ids)\n\tstreams", "\tstreams",
  "./internal/logpipe/", "TestBuildStreamsIsOrderStable"),

 ("cardinality classifies", "internal/logpipe/cardinality.go",
  "return len(ct.counts[key]) < ct.threshold", "return true",
  "./internal/logpipe/", "TestCardinalityDecidesWhetherALabelBecomesANode"),

 ("chunk id carries its prefix", "internal/logpipe/chunk.go",
  'return fmt.Sprintf("log-chunk:%x", h[:16])', 'return fmt.Sprintf("%x", h[:16])',
  "./internal/logpipe/", "TestChunkIDShape"),

 ("chunk id separator byte", "internal/logpipe/chunk.go",
  "\tb.WriteString(streamID)\n\tb.WriteByte('|')\n\tb.WriteString(templateID)\n\tb.WriteByte('|')",
  "\tb.WriteString(streamID)\n\tb.WriteString(templateID)",
  "./internal/logpipe/", "TestChunkIDShape"),

 ("chunk window is epoch-aligned", "internal/logpipe/chunk.go",
  "\tbucket := (ts.UnixNano() / w) * w\n\treturn time.Unix(0, bucket).UTC()", "\treturn ts",
  "./internal/logpipe/", "TestWindowStartIsAlignedToTheUTCEpoch"),

 ("entries in two windows are two chunks", "internal/logpipe/chunk.go",
  "WindowStart: WindowStart(e.Timestamp, window)}", "WindowStart: time.Time{}}",
  "./internal/logpipe/", "TestEntriesSpanningTwoWindowsProduceTwoChunks"),

 ("chunk parallel-slice length is checked", "internal/logpipe/chunk.go",
  "\tif len(entryStreamIDs) != len(entries) || len(entryTemplateIDs) != len(entries) {",
  "\tif false {",
  "./internal/logpipe/", "TestAssembleChunksRefusesAMismatchedParallelSlice"),

 ("chunk decode bounds-checks its payload", "internal/logpipe/chunk.go",
  "\t\t\tif pos+int(vl) > len(data) {", "\t\t\tif false {",
  "./internal/logpipe/", "TestDecodeChunkRefusesATruncatedPayload"),

 ("CONTAINS runs template to chunk", "internal/logpipe/graph.go",
  "framework.Edge{FromID: c.TemplateID, ToID: c.ID, Type: EdgeContains},",
  "framework.Edge{FromID: c.ID, ToID: c.TemplateID, Type: EdgeContains},",
  "./internal/logpipe/", "TestEdgeDirections"),

 ("chunk content is the compressed payload", "internal/logpipe/graph.go",
  "\t\tContent:  string(c.CompressedData),", "",
  "./internal/logpipe/", "TestChunkContentIsTheCompressedPayload"),

 # --- THE FIX ROUND'S T2-2: every emitted id and time value ---
 ("first_seen is the EARLIEST entry", "internal/logpipe/graph.go",
  '\t\tmeta["first_seen"] = t.FirstSeen.UTC().Format(timestampMetaLayout)',
  '\t\tmeta["first_seen"] = t.LastSeen.UTC().Format(timestampMetaLayout)',
  "./internal/logpipe/", "TestEmittedTemplateValues"),

 ("last_seen is the LATEST entry", "internal/logpipe/graph.go",
  '\t\tmeta["last_seen"] = t.LastSeen.UTC().Format(timestampMetaLayout)',
  '\t\tmeta["last_seen"] = t.FirstSeen.UTC().Format(timestampMetaLayout)',
  "./internal/logpipe/", "TestEmittedTemplateValues"),

 ("a chunk's start_time and end_time are not swapped", "internal/logpipe/graph.go",
  '\t\tmeta["start_time"] = c.StartTime.UTC().Format(timestampMetaLayout)',
  '\t\tmeta["start_time"] = c.EndTime.UTC().Format(timestampMetaLayout)',
  "./internal/logpipe/", "TestEmittedChunkValues"),

 ("a chunk's end_time is its latest entry", "internal/logpipe/graph.go",
  '\t\tmeta["end_time"] = c.EndTime.UTC().Format(timestampMetaLayout)',
  '\t\tmeta["end_time"] = c.StartTime.UTC().Format(timestampMetaLayout)',
  "./internal/logpipe/", "TestEmittedChunkValues"),

 ("a chunk's entry range is the ENTRIES' bounds", "internal/logpipe/chunk.go",
  "\t\t\tStartTime:      start,\n\t\t\tEndTime:        end,",
  "\t\t\tStartTime:      end,\n\t\t\tEndTime:        start,",
  "./internal/logpipe/", "TestEmittedChunkValues"),

 ("a template's count is its entry count", "internal/logpipe/graph.go",
  '\t\t"count":    strconv.Itoa(t.Count),',
  '\t\t"count":    "1",',
  "./internal/logpipe/", "TestEmittedTemplateValues"),

 ("a chunk names the stream and template it joins", "internal/logpipe/graph.go",
  '\t\t"stream_id":   c.StreamID,',
  '\t\t"stream_id":   c.TemplateID,',
  "./internal/logpipe/", "TestEmittedChunkValues"),

 ("a label node carries its key and value", "internal/logpipe/graph.go",
  '\t\t\tMetadata:   map[string]string{"label_key": kv[0], "label_value": kv[1]},',
  '\t\t\tMetadata:   map[string]string{"label_key": kv[1], "label_value": kv[0]},',
  "./internal/logpipe/", "TestEmittedLabelValues"),

 ("a proxy carries its foreign reference", "internal/logpipe/proxy.go",
  '\t\t"foreign_id":    r.ResourceID,',
  '\t\t"foreign_id":    r.Account,',
  "./internal/logpipe/", "TestEmittedProxyAndCorrelationValues"),

 ("template SymbolName falls back to the pattern", "internal/logpipe/graph.go",
  "\tsymbol := alias\n\tif symbol == \"\" {\n\t\tsymbol = t.Pattern\n\t}", "\tsymbol := alias",
  "./internal/logpipe/", "TestTemplateAliasFallsBackToThePattern"),

 ("stream node carries every label", "internal/logpipe/graph.go",
  "\tfor k, v := range s.Labels {\n\t\tmeta[\"label:\"+k] = v\n\t}",
  "\tfor k, v := range s.LowCardLabels {\n\t\tmeta[\"label:\"+k] = v\n\t}",
  "./internal/logpipe/", "TestStreamNodeCarriesEveryLabelIncludingTheHighCardinalityHalf"),

 ("proxy id convention", "internal/logpipe/proxy.go",
  'return "proxy:cloud:" + account + ":" + resourceID', 'return account + "/" + resourceID',
  "./internal/logpipe/", "TestSuppliedResolutionsEmitProxiesAndEmittedBy"),

 ("dangling EMITTED_BY is refused", "internal/logpipe/proxy.go",
  "\t\tif knownLabels != nil {", "\t\tif false {",
  "./internal/logpipe/", "TestResolutionNamingAnUnknownLabelIsRefused"),

 ("incomplete resolution is refused", "internal/logpipe/proxy.go",
  "\t\tif r.Account == \"\" || r.ResourceID == \"\" {", "\t\tif false {",
  "./internal/logpipe/", "TestResolutionWithNoAccountIsRefused"),

 ("proxy emission order is stated", "internal/logpipe/proxy.go",
  "\tsorted := sortedResolutions(resolutions)", "\tsorted := resolutions",
  "./internal/logpipe/", "TestProxyEmissionFollowsAStatedOrder"),

 ("proxy is deduplicated", "internal/logpipe/proxy.go",
  "\t\tif _, dup := seen[proxyID]; !dup {", "\t\tif true {",
  "./internal/logpipe/", "TestProxyIsDeduplicatedAcrossResolutions"),

 ("the consolidator remap is deterministic", "internal/logpipe/process.go",
  "\t\tif best := bestSurvivor(dropped, survivors); best != \"\" {\n\t\t\tremap[id] = best\n\t\t}",
  "\t\t_ = dropped\n\t\tfor afterID := range after {\n\t\t\tremap[id] = afterID\n\t\t}",
  "./internal/logpipe/", "TestConsolidatorRemapIsDeterministic"),

 # --- THE FIX ROUND'S T2-1: the drain overflow path ---
 ("the overflow path attributes rather than dropping", "internal/logpipe/drain.go",
  "\tbest := d.findBestGlobalMatch(tokens)",
  "\tpanic(\"MUTATION: the drain overflow path is unreachable by any test\")\n\tbest := d.findBestGlobalMatch(tokens)",
  "./internal/logpipe/", "TestDrainOverflowAttributesRatherThanDropping|TestDrainOverflowKeepsEveryEntryAttributed"),

 ("the overflow fallback to the newest cluster", "internal/logpipe/drain.go",
  "\tif best == nil {\n\t\tbest = d.clusters[len(d.clusters)-1]\n\t}",
  "\tif best == nil {\n\t\treturn nil\n\t}",
  "./internal/logpipe/", "TestDrainOverflowAttributesRatherThanDropping|TestDrainOverflowKeepsEveryEntryAttributed"),

 ("the cluster cap is honored", "internal/logpipe/drain.go",
  "\tif len(d.clusters) >= d.config.MaxClusters {",
  "\tif false {",
  "./internal/logpipe/", "TestDrainOverflowAttributesRatherThanDropping"),

 ("similarity scores a length mismatch zero", "internal/logpipe/drain.go",
  "\tif len(a) != len(b) || len(a) == 0 {\n\t\treturn 0\n\t}",
  "\tif len(a) == 0 {\n\t\treturn 0\n\t}\n\tif len(a) != len(b) {\n\t\treturn 1\n\t}",
  "./internal/logpipe/", "TestDrainSimilarity"),

 ("a cluster's time bounds widen in BOTH directions", "internal/logpipe/drain.go",
  "\tif tpl.FirstSeen.IsZero() || ts.Before(tpl.FirstSeen) {",
  "\tif tpl.FirstSeen.IsZero() {",
  "./internal/logpipe/", "TestDrainFirstAndLastSeenWidenInBothDirections"),

 ("a zero timestamp does not move a cluster's bounds", "internal/logpipe/drain.go",
  "\tif ts.IsZero() {\n\t\treturn\n\t}",
  "",
  "./internal/logpipe/", "TestDrainFirstAndLastSeenWidenInBothDirections"),

 ("example values are captured from later matches", "internal/logpipe/drain.go",
  "\tif vars != nil \u0026\u0026 len(tpl.ExampleVars) < maxExampleVars {",
  "\tif false {",
  "./internal/logpipe/", "TestDrainExampleVarsAreCapturedFromLaterMatches"),

 ("a merged template carries an id", "internal/logpipe/consolidator_gostack.go",
  "\tmerged.ID = TemplateID(merged.Pattern)\n", "",
  "./internal/logpipe/", "TestGoStackFragmentsMergeIntoOneTemplate"),

 ("ordinary lines are not consolidated", "internal/logpipe/consolidator_gostack.go",
  "\tif norm[0] == '{' || norm[0] == '[' {\n\t\treturn false\n\t}", "",
  "./internal/logpipe/", "TestEachStackFrameRejectionDiscriminates|TestOrdinaryLinesAreNotConsolidated"),

 ("an unlabelled entry is refused", "internal/logpipe/pipeline.go",
  "\t\tif len(e.Labels) == 0 {", "\t\tif len(e.Labels) < 0 {",
  "./internal/logpipe/", "TestValidateEntriesRefusesAnUnlabelledEntry"),

 ("the pod key sorts into the alias pair", "internal/k8slogs/labels.go",
  'LabelPod = "container_pod"', 'LabelPod = "pod"',
  "./internal/k8slogs/", "TestTwoPodsGetDifferentReadableNames|TestStreamAliasDiscriminatesTwoPods"),

 ("the cluster keys sort below the pod key", "internal/k8slogs/labels.go",
  'LabelCluster = "kube_cluster"', 'LabelCluster = "cluster"',
  "./internal/k8slogs/", "TestClusterKeysDoNotDisplaceThePodFromTheName"),

 ("empty label values are omitted", "internal/k8slogs/labels.go",
  "\t\tif pair[1] == \"\" {\n\t\t\tcontinue\n\t\t}", "",
  "./internal/k8slogs/", "TestEmptyLabelValuesAreOmitted"),

 ("an empty namespace list is refused", "internal/k8slogs/params.go",
  "\tif len(namespaces) == 0 {", "\tif false {",
  "./internal/k8slogs/", "TestNoNamespacesIsRefused"),

 ("an inverted time range is refused", "internal/k8slogs/params.go",
  "\tif !out.Since.IsZero() && !out.Until.IsZero() && out.Until.Before(out.Since) {", "\tif false {",
  "./internal/k8slogs/", "TestTimeRangeArms"),

 ("a non-positive bound is refused", "internal/k8slogs/params.go",
  "\tif *value <= 0 {", "\tif false {",
  "./internal/k8slogs/", "TestSelfBoundsMustBePositive"),

 ("the API's tail-lines-with-one-stream rule", "internal/k8slogs/podlog.go",
  "\tif tailLines != nil && (name == StreamStdout || name == StreamStderr) {", "\tif false {",
  "./internal/k8slogs/", "TestStreamSelectorArms"),

 ("an empty namespace never reaches the API", "internal/k8slogs/podlog.go",
  "\tif namespace == \"\" {\n\t\treturn nil, fmt.Errorf(", "\tif false {\n\t\treturn nil, fmt.Errorf(",
  "./internal/k8slogs/", "TestAnEmptyNamespaceIsRefusedBeforeItReachesTheAPI"),

 ("init containers are log sources", "internal/k8slogs/podlog.go",
  "\tfor _, c := range pod.Spec.InitContainers {\n\t\tif keep(c.Name) {\n\t\t\tout = append(out, Source{Namespace: pod.Namespace, Pod: pod.Name, Container: c.Name, Init: true})\n\t\t}\n\t}",
  "",
  "./internal/k8slogs/", "TestInitContainersAreLogSources"),

 ("timestamps are always requested", "internal/k8slogs/podlog.go",
  "\t\tTimestamps: true,", "\t\tTimestamps: false,",
  "./internal/k8slogs/", "TestTimestampsAreAlwaysRequestedAndStripped"),

 ("the end bound is applied client-side", "internal/k8slogs/podlog.go",
  "\tif !opts.Until.IsZero() && ts.After(opts.Until) {", "\tif false {",
  "./internal/k8slogs/", "TestTheEndBoundIsAppliedClientSide"),

 ("an engaged bound marks the read truncated", "internal/k8slogs/podlog.go",
  "\tresult.Truncated = boundEngaged(opts, lines, bytesRead)", "\tresult.Truncated = false",
  "./internal/k8slogs/", "TestTheSelfBoundsReachTheServerAndMarkTheReadTruncated"),

 # --- THE NO-CAPS FIX ROUND: restoring each retired bound must redden a test ---
 ("no line-length ceiling", "internal/k8slogs/podlog.go",
  "\treader := bufio.NewReader(body)",
  "\treader := bufio.NewReader(io.LimitReader(body, 8<<20))",
  "./internal/k8slogs/", "TestNoLineLengthCeiling"),

 ("truncation is measured, not inferred from a byte bound", "internal/k8slogs/podlog.go",
  "\treturn opts.LimitBytes != nil \u0026\u0026 *opts.LimitBytes > 0 \u0026\u0026 bytesRead >= *opts.LimitBytes",
  "\t_ = bytesRead\n\treturn opts.LimitBytes != nil \u0026\u0026 *opts.LimitBytes > 0",
  "./internal/k8slogs/", "TestAByteBoundIsTruncationONLYWhenItWasReached"),

 ("a byte bound that WAS reached is truncation", "internal/k8slogs/podlog.go",
  "\treturn opts.LimitBytes != nil \u0026\u0026 *opts.LimitBytes > 0 \u0026\u0026 bytesRead >= *opts.LimitBytes",
  "\t_ = bytesRead\n\treturn false",
  "./internal/k8slogs/", "TestAByteBoundIsTruncationONLYWhenItWasReached"),

 ("a line bound that WAS reached is truncation", "internal/k8slogs/podlog.go",
  "\tif opts.TailLines != nil \u0026\u0026 *opts.TailLines > 0 \u0026\u0026 int64(lines) >= *opts.TailLines {",
  "\tif false {",
  "./internal/k8slogs/", "TestALineBoundIsTruncationONLYWhenItWasReached"),

 ("an unbounded read is not truncated", "internal/k8slogs/podlog.go",
  "\tresult.Truncated = boundEngaged(opts, lines, bytesRead)", "\tresult.Truncated = true",
  "./internal/k8slogs/", "TestAnUnboundedReadIsNotTruncated"),

 ("an unparseable line is counted", "internal/k8slogs/podlog.go",
  "\t\tr.SkippedLines++", "",
  "./internal/k8slogs/", "TestAnUnparseableLineIsCountedNotSwallowed"),

 ("the stream selector reaches the API", "internal/k8slogs/podlog.go",
  "\tif stream := streamSelector(opts.Stream); stream != nil {\n\t\tpodOpts.Stream = stream\n\t}", "",
  "./internal/k8slogs/", "TestTheStreamSelectorReachesTheServer"),

 ("the read loop checks for cancellation", "internal/k8slogs/podlog.go",
  "\t\tif err := ctx.Err(); err != nil {", "\t\tif err := ctx.Err(); err != nil \u0026\u0026 false {",
  "./internal/k8slogs/", "TestTheScanLoopStopsOnCancellationMidRead"),

 ("the timestamp prefix is stripped", "internal/k8slogs/podlog.go",
  "\treturn ts.UTC(), rest, true", "\treturn ts.UTC(), prefix + \" \" + rest, true",
  "./internal/k8slogs/", "TestTimestampsAreAlwaysRequestedAndStripped"),

 # --- THE FIX ROUND'S T3s ---
 ("a pod's broken credentials get the in-cluster message", "internal/k8slogs/kubeconfig.go",
  "\tif _, inPod := os.LookupEnv(EnvKubernetesServiceHost); inPod {",
  "\tif _, inPod := os.LookupEnv(EnvKubernetesServiceHost); inPod \u0026\u0026 false {",
  "./internal/k8slogs/", "TestAPodWithBrokenCredentialsGetsTheInClusterMessage"),

 ("a cancelled container read fails the collect", "internal/k8slogs/collector.go",
  "\t\t\t\t\tif ctxErr := ctx.Err(); ctxErr != nil {\n\t\t\t\t\t\treturn nil, nil, err\n\t\t\t\t\t}",
  "",
  "./internal/k8slogs/", "TestACancelledCollectStopsRatherThanReportingAPartialWalk"),

 ("a cancelled LIST fails the collect", "internal/k8slogs/collector.go",
  "\t\t\tif ctxErr := ctx.Err(); ctxErr != nil {\n\t\t\t\treturn nil, nil, fmt.Errorf(\n\t\t\t\t\t\"k8s-logs: the collect was cancelled while listing namespace %q: %w\", ns, ctxErr)\n\t\t\t}",
  "",
  "./internal/k8slogs/", "TestACancelledCollectStopsRatherThanReportingAPartialWalk"),

 ("the python header truncation holds its shape", "internal/logpipe/consolidator_python.go",
  "const pythonHeaderLimit = 120", "const pythonHeaderLimit = 12",
  "./internal/logpipe/", "TestThePythonHeaderTruncationHoldsItsShape"),

 ("the README ships the generated entry", "internal/k8slogs/env.go",
  '\t\t"tool":    ToolName,', '\t\t"tool":    ToolName + "_v2",',
  "./internal/k8slogs/", "TestTheREADMEShipsTheGeneratedEntry"),

 ("a named context must exist", "internal/k8slogs/kubeconfig.go",
  "\t\tif err := r.assertContextExists(rules, contextName); err != nil {\n\t\t\treturn nil, err\n\t\t}", "",
  "./internal/k8slogs/", "TestAMissingNamedContextFailsLoudNamingIt"),

 ("the injected in-cluster loader is used", "internal/k8slogs/kubeconfig.go",
  "\tif r.InCluster != nil {\n\t\treturn r.InCluster()\n\t}", "",
  "./internal/k8slogs/", "TestInClusterConfigIsReturnedAndNotFallenThroughFrom"),

 ("the in-cluster failure is its own message", "internal/k8slogs/kubeconfig.go",
  '"k8s-logs: the in-cluster credentials did not resolve (%v); "+',
  '"k8s-logs: no kubeconfig and the in-cluster credentials did not resolve (%v); "+',
  "./internal/k8slogs/", "TestInClusterFailureIsDistinguishableFromTheCombinedMessage"),

 ("the GKE context parse", "internal/k8slogs/kubeconfig.go",
  "\treturn parts[0], parts[len(parts)-1]", '\treturn "", ""',
  "./internal/k8slogs/", "TestParseKubeContext|TestTheContextNameBecomesTheClusterLabels"),

 ("the windows environment arm", "internal/k8slogs/env.go",
  "\t\tnames = append(names, EnvHomeDrive, EnvHomePath, EnvUserProfile, EnvSystemRoot)", "",
  "./internal/k8slogs/", "TestEnvTablePerTargetOS|TestWindowsAddsTheHomeFallbacksAndTheSpawnRoot"),

 ("an unknown target OS is refused", "internal/k8slogs/env.go",
  "\tdefault:\n\t\treturn nil, fmt.Errorf(", "\tdefault:\n\t\t_ = targetOS\n\tcase \"unreachable\":\n\t\treturn nil, fmt.Errorf(",
  "./internal/k8slogs/", "TestUnknownTargetOSIsRefused"),

 ("chunk content is excluded graph-wide", "internal/k8slogs/env.go",
  'GraphBM25Fields      = []string{"symbol_name", "description", "summary"}',
  'GraphBM25Fields      = []string{"symbol_name", "description", "summary", "content"}',
  "./internal/k8slogs/", "TestBehaviorDeclaresAllThreeAndExcludesChunkContent"),

 ("the entry declares syncable", "internal/k8slogs/env.go",
  '\t\t\t\t\t"syncable":         true,\n', "",
  "./internal/k8slogs/", "TestBehaviorDeclaresAllThreeAndExcludesChunkContent"),

 ("the entry declares summarizable and embeddable", "internal/k8slogs/env.go",
  '\t\t\t\t\t"summarizable":     true,\n\t\t\t\t\t"embeddable":       true,\n', "",
  "./internal/k8slogs/", "TestBehaviorDeclaresAllThreeAndExcludesChunkContent"),

 ("the log-chunk override", "internal/k8slogs/env.go",
  '\t\t"log-chunk": map[string]any{\n\t\t\t"embeddable":  false,\n\t\t\t"bm25_fields": []string{"symbol_name"},\n\t\t},',
  "",
  "./internal/k8slogs/", "TestBehaviorDeclaresAllThreeAndExcludesChunkContent"),

 ("env values are ${VAR} references", "internal/k8slogs/env.go",
  '\t\tenv[n] = "${" + n + "}"', '\t\tenv[n] = "/home/operator/.kube/config"',
  "./internal/k8slogs/", "TestExampleEntryHasTheShapeTheLoaderRequires|TestTheREADMEShipsTheGeneratedEntry"),

 ("the walk reports incompleteness", "internal/k8slogs/collector.go",
  "\tif len(reasons) == 0 {\n\t\treturn framework.Complete()\n\t}", "\tif true {\n\t\treturn framework.Complete()\n\t}",
  "./internal/k8slogs/", "TestWalkCompleteArms"),

 ("a listing failure does not fail the collect", "internal/k8slogs/collector.go",
  "\t\t\tincomplete = append(incomplete, fmt.Sprintf(\"namespace %s could not be listed: %v\", ns, err))\n\t\t\tcontinue",
  "\t\t\treturn nil, nil, err",
  "./internal/k8slogs/", "TestAListingFailureIsReportedRatherThanFailingTheCollect"),

 ("the per-container stream is closed", "internal/k8slogs/podlog.go",
  "\tdefer func() { _ = body.Close() }()",
  "",
  "./internal/k8slogs/", "TestTheStreamIsClosedOnBothPaths"),

 # --- THE FIX ROUND'S T1: every read outcome reaches the verdict truthfully ---
 ("a container that could not be read makes the walk incomplete", "internal/k8slogs/collector.go",
  "\t\t\t\t\tincomplete = append(incomplete, fmt.Sprintf(\n\t\t\t\t\t\t\"container %s of pod %s/%s could not be read: %v\", src.Container, src.Namespace, src.Pod, err))",
  "\t\t\t\t\t_ = err",
  "./internal/k8slogs/", "TestReadOutcomeDenied|TestReadOutcomePodGoneMidRead|TestReadOutcomeContainerRestarted|TestReadOutcomeStreamErrorMidRead"),

 ("a mid-body stream failure is an error, not a silent break", "internal/k8slogs/podlog.go",
  "\t\t\treturn result, fmt.Errorf(\n\t\t\t\t\"k8s-logs: the log stream of container %s in pod %s/%s failed after %d lines: %w\",\n\t\t\t\tsrc.Container, src.Namespace, src.Pod, lines, readErr)",
  "\t\t\tbreak",
  "./internal/k8slogs/", "TestAPartialStreamIsNotPassedOffAsWhole|TestReadOutcomeStreamErrorMidRead"),

 ("an empty container does NOT make the walk incomplete", "internal/k8slogs/collector.go",
  "\t\t\t\tif read.SkippedLines > 0 {",
  "\t\t\t\tif true {",
  "./internal/k8slogs/", "TestReadOutcomeEmptyContainer|TestReadOutcomeSuccess"),

 ("the context parameter reaches the client", "internal/k8slogs/collector.go",
  "\t\treturn c.NewClient(contextName)", '\t\treturn c.NewClient("")',
  "./internal/k8slogs/", "TestTheContextParameterReachesTheClientBuilder"),

 # --- THE STANDALONE go.mod PIN ---
 # THE SUBJECT OF THIS PIN IS go.mod, NOT THE TEST, so the mutation is the
 # defect itself: drop the require the workspace would resolve anyway. No
 # mutation of the TEST reds on a correct tree — with go.mod tidied, tidy -diff
 # is clean in either environment and the assertion has nothing to catch. That
 # is the ordinary shape of a gate, and it is why the mutation is on the input.
 ("go.mod is complete outside the workspace", "go.mod",
  "\tgithub.com/stretchr/testify v1.11.1\n",
  "",
  ".", "TestGoModIsCompleteStandalone"),

 # --- THE ADOPTION REVIEW'S FIX ROUND ---
 ("the emitted edge's method bytes are pinned", "../common/correlation/materialize.go",
  'CorrelationMethod = "temporal+cloud-dependency"',
  'CorrelationMethod = "temporal+cloud-dep"',
  "./internal/logpipe/", "TestEmittedProxyAndCorrelationValues|TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo"),

 ("the emitted edge's type bytes are pinned", "../common/correlation/materialize.go",
  'EdgeCorrelatesWith = "CORRELATES_WITH"',
  'EdgeCorrelatesWith = "CORRELATES_WITH_X"',
  "./internal/logpipe/", "TestEmittedProxyAndCorrelationValues|TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo"),

 ("the edge's own graph is part of the key", "internal/k8slogs/cloudcontext.go",
  "func dependencyKey(graph, from, to string) [3]string { return [3]string{graph, from, to} }",
  "func dependencyKey(graph, from, to string) [3]string { _ = graph; return [3]string{\"\", from, to} }",
  "./internal/k8slogs/", "TestAnEdgeDeclaredInOneGraphDoesNotConfirmAPairFromAnother"),

 ("a typed nil never reaches the detector", "internal/k8slogs/collector.go",
  "\t\tif resolver := cloud.Correlation(streams); resolver != nil {\n\t\t\topts.Resolver = resolver\n\t\t\topts.Oracle = resolver\n\t\t}",
  "\t\tresolver := cloud.Correlation(streams)\n\t\topts.Resolver = resolver\n\t\topts.Oracle = resolver",
  "./internal/k8slogs/", "TestACloudSliceThatResolvesNothingLeavesTheDetectorUnreached"),

 ("a nil chunk is projected, not dropped", "internal/logpipe/correlate.go",
  "\t\tif c == nil {\n\t\t\tout = append(out, nil)\n\t\t\tcontinue\n\t\t}",
  "\t\tif c == nil {\n\t\t\tcontinue\n\t\t}",
  "./internal/logpipe/", "TestANilChunkAndANilStreamAreProjectedRatherThanDropped"),

 ("a nil stream is projected, not dropped", "internal/logpipe/correlate.go",
  "\t\tif s == nil {\n\t\t\tout = append(out, nil)\n\t\t\tcontinue\n\t\t}",
  "\t\tif s == nil {\n\t\t\tcontinue\n\t\t}",
  "./internal/logpipe/", "TestANilChunkAndANilStreamAreProjectedRatherThanDropped"),

 # --- THE COMMON CORRELATION MODULE: the projection and the two cloud answers ---
 ("only a confirmed correlation becomes an edge", "internal/logpipe/pipeline.go",
  "\tedges = append(edges, correlation.MaterializeCorrelations(correlations)...)",
  "\tfor i := range correlations {\n\t\tcorrelations[i].StructurallyConfirmed = true\n\t}\n\tedges = append(edges, correlation.MaterializeCorrelations(correlations)...)",
  ".", "TestARoundTripEmitsAConfirmedCorrelation"),

 ("the correlation detector is reached at all", "internal/logpipe/pipeline.go",
  "\tcorrelations, err := findCorrelations(templates, chunks, streams, opts)",
  "\tcorrelations, err := []correlation.Result(nil), error(nil)\n\t_ = findCorrelations",
  ".", "TestARoundTripEmitsAConfirmedCorrelation"),

 ("the resolver and oracle reach the detector", "internal/k8slogs/collector.go",
  "\t\tif resolver := cloud.Correlation(streams); resolver != nil {",
  "\t\tif resolver := cloud.Correlation(streams); false {",
  ".", "TestARoundTripEmitsAConfirmedCorrelation"),

 ("the oracle answers in both directions", "internal/k8slogs/cloudcontext.go",
  "\t\t\tdependsOn[dependencyKey(g.GraphName, e.ToID, e.FromID)] = struct{}{}\n",
  "",
  "./internal/k8slogs/", "TestTheOracleAnswersFromTheSuppliedEdges"),

 ("the oracle keeps the account rule", "internal/k8slogs/cloudcontext.go",
  "\tif a.Account != b.Account {",
  "\tif false {",
  "./internal/k8slogs/", "TestTheOracleAnswersFromTheSuppliedEdges"),

 ("the proxy map reaches the evidence string", "internal/k8slogs/collector.go",
  "\t\topts.ProxyMap = cloud.ProxyMap(streams)",
  "",
  ".", "TestARoundTripEmitsAConfirmedCorrelation"),

 ("the severity vocabulary matches the common detector's", "internal/logpipe/severity.go",
  "\tSeverityError:    4,",
  "\tSeverityError:    1,",
  "./internal/logpipe/", "TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo"),

 ("a nil element is projected, not dropped", "internal/logpipe/correlate.go",
  "\t\tif t == nil {\n\t\t\tout = append(out, nil)\n\t\t\tcontinue\n\t\t}",
  "\t\tif t == nil {\n\t\t\tcontinue\n\t\t}",
  "./internal/logpipe/", "TestANilTemplateIsProjectedRatherThanDropped"),

 # --- THE LANDING REBASE: the declared context block's route ---
 ("the declared context block reaches the walk", "internal/k8slogs/collector.go",
  "\tresult, err := c.build(entries, resolved, CloudContextFrom(foreign))",
  "\t_ = foreign\n\tresult, err := c.build(entries, resolved, CloudContext{})",
  ".", "TestARoundTripCarriesTheDeclaredCloudContext"),

 ("the cloud context is read", "internal/k8slogs/collector.go",
  "\tif !cloud.IsEmpty() {", "\tif false {",
  "./internal/k8slogs/", "TestAWalkWithACloudContextEmitsProxiesAndEmittedBy|TestAWalkWithACloudContextEmitsAConfirmedCorrelation"),

 ("the cluster must agree", "internal/k8slogs/cloudcontext.go",
  "\t\t\t\tif _, match := clusters[cluster]; !match {", "\t\t\t\tif _, match := clusters[cluster]; !match \u0026\u0026 false {",
  "./internal/k8slogs/", "TestTheClusterMustAgreeWhereBothSidesKnowIt"),

 ("an ambiguous namespace is dropped", "internal/k8slogs/cloudcontext.go",
  "\t\tif n > 1 {", "\t\tif n > 1 \u0026\u0026 false {",
  "./internal/k8slogs/", "TestANamespaceResolvingToSeveralResourcesIsDroppedFromCorrelation"),

 ("stdout carries protocol only", "internal/k8slogs/collector.go",
  "\tresolved, err := params.Validate()",
  '\tfmt.Fprintln(os.Stdout, "diagnostic on stdout")\n\tresolved, err := params.Validate()',
  ".", "TestStdioRoundTripListsTheToolAndReturnsAValidatedResult"),
]


def run(pkg, pattern):
    # -v so the runner's own PASS/FAIL lines are readable: `go test -run` with a
    # pattern that matches NOTHING exits 0, so a mutation whose test name has
    # since been renamed would read as an unobserved-but-green declaration.
    return subprocess.run(
        ["go", "test", "-v", pkg, "-run", pattern],
        cwd=ROOT, capture_output=True, text=True)


def ran_any(out):
    return "=== RUN" in out


def main():
    only = sys.argv[1] if len(sys.argv) > 1 else None
    failures = []
    for label, rel, old, new, pkg, pattern in M:
        if only and only not in label:
            continue
        path = os.path.join(ROOT, rel)
        if not os.path.exists(path):
            # A file this harness names and the tree does not carry. It is the
            # same class as a moved anchor and it is reported the same way: a
            # traceback here would abandon every mutation after it, so the run
            # that was meant to certify the whole set would certify a prefix.
            print(f"SETUP-ERROR {label}: {rel} does not exist; the subject moved or was deleted")
            failures.append(label)
            continue
        with open(path) as fh:
            src = fh.read()
        if src.count(old) != 1:
            print(f"SETUP-ERROR {label}: anchor occurs {src.count(old)} times in {rel}")
            failures.append(label)
            continue
        try:
            with open(path, "w") as fh:
                fh.write(src.replace(old, new, 1))
            if label == "stdout carries protocol only":
                with open(path) as fh:
                    s2 = fh.read()
                with open(path, "w") as fh:
                    fh.write(s2.replace('import (\n\t"context"', 'import (\n\t"context"\n\t"os"'))
            r = run(pkg, pattern)
            if not ran_any(r.stdout):
                # Either the pattern matched nothing (go test -run exits 0 on an
                # empty selection) or the mutated package no longer compiles. A
                # compile failure is a red, but a weaker one: it says the edit was
                # malformed rather than that a test observes the declaration.
                print(f"NO-TEST-RAN {label}: {pattern!r} selected no test, or the mutation broke the build")
                print("      " + (r.stdout + r.stderr).strip().splitlines()[0][:160])
                failures.append(label)
                continue
            reddened = r.returncode != 0
            verdict = "RED  " if reddened else "GREEN"
            print(f"{verdict} {label}  ->  {pattern}")
            if not reddened:
                print("      the mutation left the suite green; the declaration is unobserved")
                failures.append(label)
        finally:
            with open(path, "w") as fh:
                fh.write(src)
    print()
    if failures:
        print(f"UNOBSERVED DECLARATIONS: {len(failures)}")
        for f in failures:
            print(f"  {f}")
        sys.exit(1)
    print("every mutation reddened a named test")


if __name__ == "__main__":
    main()
