#!/bin/sh
# install.sh — one-command installer for a knowledge collector.
#
#   curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- aws
#
# Thin POSIX-sh bootstrap (no bashisms): detect os/arch, resolve the release,
# download that collector's archive and checksums.txt, sha256-verify the
# archive, place the bare binary in ~/.knowledge/bin, record the tag in a
# sidecar beside it, and register it with `knowledge collector add` so a
# following collect needs only the provider's credentials in the environment.
#
# Flags: --version <tag> pins a release (it does NOT print a version — the
# spelling matches the client's own installer), --print-env-table prints the
# per-collector environment table and exits, --print-env-sensitive-table prints
# the per-collector empty-sensitive names and exits, --help.
#
# Credentials: none are read from argv and NO CREDENTIAL VALUE IS WRITTEN
# ANYWHERE. A provider secret this shell holds reaches the config entry as a bare
# `${NAME}` REFERENCE to the operator's own environment — never as its value, and
# never as a `${NAME:-}` default. The two spellings are not interchangeable and
# the difference is the whole of this rule: the defaulted form EXPANDS TO THE
# EMPTY STRING in a process that does not hold the name, so the collector is
# handed the name present and empty and reports a permissions failure; the bare
# form is REFUSED BY NAME at collect time, naming the file, the entry, the field
# and the variable, and that refusal is scoped to the entry that carries it. A
# literal that is a URL carrying userinfo is refused rather than written. A GitHub
# token for a private release is read from GH_TOKEN, then GITHUB_TOKEN, and is
# never placed in argv, in the entry, in the sidecar, in any file, or in any
# message.

set -eu

REPO="fulminate-io/knowledge-contrib"
GITHUB_API="https://api.github.com"
INSTALL_DIR="${HOME:-}/.knowledge/bin"
COLLECTORS="aws azure bitbucket-pipelines cloudwatch gcp github-actions gitlab-ci k8s k8s-logs loki stackdriver"

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

usage() {
	echo "usage: install.sh <collector> [--version <tag>]"
	echo "       install.sh --print-env-table"
	echo "       install.sh --print-env-sensitive-table"
	echo "collectors: $COLLECTORS"
}

# --- (1) the per-collector table -------------------------------------------
#
# THREE CLASSES, and the class decides what is written rather than only what the
# variable means:
#
#   path      a LITERAL from the installing shell. The entry's env block is the
#             collector's WHOLE environment, so without these the provider's
#             file-based credential chains resolve nothing.
#   selector  a LITERAL when set; the key is OMITTED ENTIRELY when unset. An
#             empty selector is not the same as an absent one: it selects the
#             thing named by the empty string, which resolves nothing.
#   secret    A BARE `${NAME}` REFERENCE when this shell holds the name; nothing
#             at all when it does not. Never the value, and never a `${NAME:-}`
#             default. The operator's own environment supplies the value at
#             spawn, so the credential lives in no file.
#
# WHY THE REFERENCE IS BARE AND NOT DEFAULTED, and it took a measurement to see:
# a `${NAME:-}` reference is expanded by the process SERVING the collect, whose
# environment under a service manager holds PATH and nothing else, so it reaches
# the collector as the name PRESENT AND EMPTY on every collect. The bare form has
# no such default to fall back to, so the same process REFUSES the entry by name
# instead — naming the file, the entry, the field and the variable — and every
# other entry in that file keeps collecting. A refusal an operator can act on is
# what this class is choosing over a credential that silently arrives empty.
#
# WHICH COLLECTORS TELL THAT STATE APART FROM ABSENT IS DECLARED, NOT COUNTED
# HERE. Every collector marks each name it discriminates on with
# `empty_sensitive` in its own describe declaration, and
# --print-env-sensitive-table below prints the marks; the shipped-document gate in
# scripts/collector-install_test.sh reads that table rather than a list of its
# own. The earlier version of this comment named three collectors and one of the
# three had no discriminating name at all, which is what a hand-maintained count
# beside a derived table turns into.
#
# WHY THE ROW IS WRITTEN ONLY WHEN THIS SHELL HOLDS THE NAME, which is the same
# set-ness gate the selector class uses. A collector's secret rows are frequently
# ALTERNATIVES rather than a set to satisfy together — one names a token and its
# other common spelling, another names a bearer token beside a username and
# password — so a reference written for every declared secret would demand that
# an operator set names their authentication mode does not use, and would fail
# the add-time dial outright for the ones they cannot supply. The name they DID
# export is the one they authenticate with.
#
# AND NO LITERAL CARRIES A CREDENTIAL. A path or selector value that is a URL
# with userinfo is refused by name rather than written or rewritten; see
# refuse_userinfo below.
#
# PROVENANCE IS THE COLLECTOR ITSELF. Every row below is DERIVED from the
# collector's own declaration — the one its required `describe` tool serves and
# its binary answers on `--describe-env-table` — by
# scripts/gen-collector-tables.sh, and a census gates the two against each other.
# The table used to be written here by hand against each module's environment
# source, which made a variable a module started reading three edits in three
# places and a variable it stopped reading a row nobody deleted.
# --- BEGIN GENERATED TABLES (scripts/gen-collector-tables.sh) ---
# Every line below is DERIVED from the collectors' own declarations by
# scripts/gen-collector-tables.sh. Do not edit it by hand: a hand edit is
# reverted by the next run and reported by the census that gates it.
collector_env_table() {
	case "$1" in
	aws)
		cat <<-'EOF'
			path AWS_CONFIG_FILE
			path AWS_SHARED_CREDENTIALS_FILE
			path HOME
			selector AWS_PROFILE
			selector AWS_REGION
			secret AWS_ACCESS_KEY_ID
			secret AWS_SECRET_ACCESS_KEY
			secret AWS_SESSION_TOKEN
		EOF
		;;
	azure)
		cat <<-'EOF'
			path HOME
			path PATH
			selector AZURE_ADDITIONALLY_ALLOWED_TENANTS
			selector AZURE_AUTHORITY_HOST
			selector AZURE_CLIENT_CERTIFICATE_PATH
			selector AZURE_CLIENT_ID
			selector AZURE_CLIENT_SEND_CERTIFICATE_CHAIN
			selector AZURE_FEDERATED_TOKEN_FILE
			selector AZURE_POD_IDENTITY_AUTHORITY_HOST
			selector AZURE_REGIONAL_AUTHORITY_NAME
			selector AZURE_SDK_GO_LOGGING
			selector AZURE_SUBSCRIPTION_ID
			selector AZURE_TENANT_ID
			selector AZURE_TOKEN_CREDENTIALS
			selector DEFAULT_IDENTITY_CLIENT_ID
			selector HTTPS_PROXY
			selector HTTP_PROXY
			selector IDENTITY_ENDPOINT
			selector IDENTITY_SERVER_THUMBPRINT
			selector IMDS_ENDPOINT
			selector MSAL_FORCE_REGION
			selector MSI_ENDPOINT
			selector NO_PROXY
			selector REGION_NAME
			selector SSL_CERT_DIR
			selector SSL_CERT_FILE
			selector http_proxy
			selector https_proxy
			selector no_proxy
			secret AZURE_CLIENT_CERTIFICATE_PASSWORD
			secret AZURE_CLIENT_SECRET
			secret AZURE_PASSWORD
			secret AZURE_USERNAME
			secret IDENTITY_HEADER
			secret MSI_SECRET
		EOF
		;;
	bitbucket-pipelines)
		cat <<-'EOF'
			selector BITBUCKET_PIPELINE_HISTORY_DEPTH
			secret BITBUCKET_APP_PASSWORD
			secret BITBUCKET_USERNAME
		EOF
		;;
	cloudwatch)
		cat <<-'EOF'
			path AWS_CONFIG_FILE
			path AWS_SHARED_CREDENTIALS_FILE
			path HOME
			selector AWS_PROFILE
			selector AWS_REGION
			secret AWS_ACCESS_KEY_ID
			secret AWS_SECRET_ACCESS_KEY
			secret AWS_SESSION_TOKEN
		EOF
		;;
	gcp)
		cat <<-'EOF'
			path HOME
			selector EXPERIMENTAL_GOOGLE_API_USE_S2A
			selector GCE_METADATA_HOST
			selector GOOGLE_API_CERTIFICATE_CONFIG
			selector GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB
			selector GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB
			selector GOOGLE_API_USE_CLIENT_CERTIFICATE
			selector GOOGLE_API_USE_MTLS
			selector GOOGLE_API_USE_MTLS_ENDPOINT
			selector GOOGLE_APPLICATION_CREDENTIALS
			selector GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED
			selector GOOGLE_CLOUD_PROJECT
			selector GOOGLE_CLOUD_QUOTA_PROJECT
			selector GOOGLE_CLOUD_UNIVERSE_DOMAIN
			selector HTTPS_PROXY
			selector HTTP_PROXY
			selector NO_PROXY
			selector S2A_TIMEOUT
			selector SSL_CERT_DIR
			selector SSL_CERT_FILE
			selector http_proxy
			selector https_proxy
			selector no_proxy
		EOF
		;;
	github-actions)
		cat <<-'EOF'
			secret GH_TOKEN
			secret GITHUB_TOKEN
		EOF
		;;
	gitlab-ci)
		cat <<-'EOF'
			selector GITLAB_URL
			secret GITLAB_PRIVATE_TOKEN
			secret GITLAB_TOKEN
		EOF
		;;
	k8s)
		cat <<-'EOF'
			path CLOUDSDK_CONFIG
			path HOME
			path KUBECONFIG
			path PATH
		EOF
		;;
	k8s-logs)
		cat <<-'EOF'
			path HOME
			path KUBECONFIG
			path PATH
		EOF
		;;
	loki)
		cat <<-'EOF'
			selector HTTPS_PROXY
			selector HTTP_PROXY
			selector LOKI_BEARER_TOKEN_FILE
			selector LOKI_CA_CERT_PATH
			selector LOKI_CLIENT_CERT_PATH
			selector LOKI_CLIENT_KEY_PATH
			selector LOKI_CLIENT_MAX_BACKOFF
			selector LOKI_CLIENT_MIN_BACKOFF
			selector LOKI_CLIENT_RETRIES
			selector LOKI_ENV_PROXY
			selector LOKI_HTTP_COMPRESSION
			selector LOKI_HTTP_PROXY_URL
			selector LOKI_NO_CACHE
			selector LOKI_ORG_ID
			selector LOKI_QUERY_TAGS
			selector LOKI_TLS_SKIP_VERIFY
			selector NO_PROXY
			selector SSL_CERT_DIR
			selector SSL_CERT_FILE
			selector http_proxy
			selector https_proxy
			selector no_proxy
			secret LOKI_AUTH_HEADER
			secret LOKI_BEARER_TOKEN
			secret LOKI_PASSWORD
			secret LOKI_USERNAME
		EOF
		;;
	stackdriver)
		cat <<-'EOF'
			path HOME
			selector GCE_METADATA_HOST
			selector GOOGLE_APPLICATION_CREDENTIALS
			selector GOOGLE_CLOUD_PROJECT
			selector GOOGLE_CLOUD_QUOTA_PROJECT
			selector HTTPS_PROXY
			selector HTTP_PROXY
			selector NO_PROXY
			selector SSL_CERT_DIR
			selector SSL_CERT_FILE
			selector http_proxy
			selector https_proxy
			selector no_proxy
		EOF
		;;
	*) return 1 ;;
	esac
}

collector_tool() {
	case "$1" in
	aws) echo "collect_aws" ;;
	azure) echo "collect" ;;
	bitbucket-pipelines) echo "collect" ;;
	cloudwatch) echo "collect" ;;
	gcp) echo "collect" ;;
	github-actions) echo "collect" ;;
	gitlab-ci) echo "collect" ;;
	k8s) echo "collect" ;;
	k8s-logs) echo "collect_k8s_logs" ;;
	loki) echo "collect_loki_logs" ;;
	stackdriver) echo "collect" ;;
	*) return 1 ;;
	esac
}

collector_env_sensitive() {
	case "$1" in
	aws)
		cat <<-'EOF'
		EOF
		;;
	azure)
		cat <<-'EOF'
			AZURE_TOKEN_CREDENTIALS
		EOF
		;;
	bitbucket-pipelines)
		cat <<-'EOF'
			BITBUCKET_PIPELINE_HISTORY_DEPTH
		EOF
		;;
	cloudwatch)
		cat <<-'EOF'
		EOF
		;;
	gcp)
		cat <<-'EOF'
		EOF
		;;
	github-actions)
		cat <<-'EOF'
		EOF
		;;
	gitlab-ci)
		cat <<-'EOF'
		EOF
		;;
	k8s)
		cat <<-'EOF'
		EOF
		;;
	k8s-logs)
		cat <<-'EOF'
			KUBERNETES_SERVICE_HOST
		EOF
		;;
	loki)
		cat <<-'EOF'
			LOKI_AUTH_HEADER
			LOKI_BEARER_TOKEN
			LOKI_BEARER_TOKEN_FILE
			LOKI_CA_CERT_PATH
			LOKI_CLIENT_CERT_PATH
			LOKI_CLIENT_KEY_PATH
			LOKI_CLIENT_MAX_BACKOFF
			LOKI_CLIENT_MIN_BACKOFF
			LOKI_CLIENT_RETRIES
			LOKI_ENV_PROXY
			LOKI_HTTP_COMPRESSION
			LOKI_HTTP_PROXY_URL
			LOKI_NO_CACHE
			LOKI_ORG_ID
			LOKI_PASSWORD
			LOKI_QUERY_TAGS
			LOKI_TLS_SKIP_VERIFY
			LOKI_USERNAME
		EOF
		;;
	stackdriver)
		cat <<-'EOF'
		EOF
		;;
	*) return 1 ;;
	esac
}
# --- END GENERATED TABLES ---

# --- (2) argument parse ----------------------------------------------------
COLLECTOR=""
PIN_VERSION=""
while [ $# -gt 0 ]; do
	case "$1" in
	--print-env-table)
		for c in $COLLECTORS; do
			collector_env_table "$c" | while read -r cls nm; do
				[ -n "$nm" ] || continue
				printf '%s\t%s\t%s\n' "$c" "$cls" "$nm"
			done
		done
		exit 0
		;;
	# THE EMPTY-SENSITIVE NAMES, one `collector<TAB>name` row per mark.
	#
	# WHY IT IS A SECOND OUTPUT RATHER THAN A FOURTH COLUMN ON THE ONE ABOVE.
	# That table's rows are read by four consumers, and one of them greps a WHOLE
	# LINE with a trailing anchor; an appended field stops matching whether it is
	# populated or empty, and every collector declaring the matched row would then
	# take the wrong branch. A separate output moves no existing reader.
	#
	# AND IT CARRIES EVERY CLASS, WHICH THE TABLE ABOVE CANNOT. The entry-writing
	# table drops the not-carried class by construction, because this script's own
	# case arm knows three tokens and fails a fourth by name — but a name no
	# installed entry carries can still appear in a worked entry an operator copies,
	# and one shipped collector's only mark is on such a name. It is therefore the
	# only place that mark could go.
	#
	# A COLLECTOR THAT MARKS NOTHING PRINTS NO ROW, and that is the answer rather
	# than a failure: most collectors treat every name's empty value as absent.
	--print-env-sensitive-table)
		for c in $COLLECTORS; do
			collector_env_sensitive "$c" | while read -r nm; do
				[ -n "$nm" ] || continue
				printf '%s\t%s\n' "$c" "$nm"
			done
		done
		exit 0
		;;
	--help | -h)
		usage
		exit 0
		;;
	--version)
		shift
		PIN_VERSION="${1:-}"
		[ -n "$PIN_VERSION" ] || fail "--version requires a tag argument"
		;;
	--version=*) PIN_VERSION="${1#--version=}" ;;
	-*) fail "unknown flag: $1" ;;
	*)
		[ -z "$COLLECTOR" ] || fail "one collector at a time: already given '$COLLECTOR', then '$1'"
		COLLECTOR="$1"
		;;
	esac
	shift
done

[ -n "$COLLECTOR" ] || {
	usage >&2
	fail "which collector? name one."
}
collector_tool "$COLLECTOR" >/dev/null 2>&1 ||
	fail "unknown collector '$COLLECTOR' — known collectors: $COLLECTORS"
TOOL="$(collector_tool "$COLLECTOR")"

# --- (3) detect os/arch ----------------------------------------------------
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
Linux) os="linux" ;;
Darwin) os="darwin" ;;
CYGWIN* | MINGW* | MSYS* | Windows_NT)
	fail "native Windows is not installable by this script — the release does publish windows-amd64 .zip archives; unpack one by hand, or use WSL (https://learn.microsoft.com/windows/wsl/)."
	;;
*) fail "unsupported OS '$os' — use WSL (https://learn.microsoft.com/windows/wsl/) or download the archive manually." ;;
esac
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) fail "unsupported architecture '$arch'." ;;
esac
if [ "$os" = "darwin" ] && [ "$arch" = "amd64" ]; then
	fail "darwin-amd64 (Intel Mac) is not a published release target — build from source or use an arm64 Mac."
fi
PLATFORM="${os}-${arch}"

# --- (4) HOME, and the client ----------------------------------------------
#
# HOME IS VALIDATED BEFORE ANYTHING IS FETCHED. It is written into the entry as
# a literal for every collector whose module declares it, and a HOME that is not
# a directory produces a collector that resolves no credential file and reports
# it as a provider error.
[ -n "${HOME:-}" ] || fail "HOME is unset; it is where collectors are installed and what the config entry declares."
[ -d "$HOME" ] || fail "HOME ($HOME) is not an existing directory — refusing to install against it."

if [ -x "$INSTALL_DIR/knowledge" ]; then
	CLIENT="$INSTALL_DIR/knowledge"
elif command -v knowledge >/dev/null 2>&1; then
	CLIENT="$(command -v knowledge)"
else
	fail "the knowledge client is not installed — install it first: curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-mcp/main/install.sh | sh"
fi

BIN_NAME="knowledge-collector-${COLLECTOR}"
DEST="$INSTALL_DIR/$BIN_NAME"
SIDECAR="$DEST.version"

# --- (5) the GitHub token, and the fetch helpers ---------------------------
#
# GH_TOKEN wins over GITHUB_TOKEN, matching the GitHub CLI's own order. Neither
# is required: a public release needs none.
GH_AUTH="${GH_TOKEN:-}"
[ -n "$GH_AUTH" ] || GH_AUTH="${GITHUB_TOKEN:-}"

# fetch <url> <out-file> <accept> — writes the body to out-file and PRINTS the
# HTTP status. It never exits on a status, so the caller can tell a 404 from a
# transport failure and say something true about which.
#
# THE TOKEN GOES IN ON STDIN, NOT IN ARGV. `curl -K -` reads the header from a
# config on standard input, so the value never appears in this process's command
# line, in `ps`, or in any file. Do not "simplify" this to -H "Authorization:
# Bearer $GH_AUTH": that publishes the token to every user on the machine.
fetch() {
	f_url="$1"
	f_out="$2"
	f_accept="$3"
	if [ -n "$GH_AUTH" ]; then
		printf 'header = "Authorization: Bearer %s"\n' "$GH_AUTH" |
			curl -sSL -K - -H "Accept: $f_accept" -o "$f_out" -w '%{http_code}' "$f_url" || true
	else
		curl -sSL -H "Accept: $f_accept" -o "$f_out" -w '%{http_code}' "$f_url" || true
	fi
}

# refuse_fetch <status> <what> — the shared refusal. A 404 with no token is the
# private-release case and names the variable to set. A 404 WITH a token is NOT
# attributed to the token: an authenticated request against a repository that
# simply has no such release returns the same 404, so both causes are named.
refuse_fetch() {
	case "$1" in
	404)
		if [ -z "$GH_AUTH" ]; then
			fail "$2 returned 404 and no token is set — set GH_TOKEN for a private release, or check the tag."
		fi
		fail "$2 returned 404 with a token set: the release or tag was not found on $REPO — either the tag does not exist, or the token cannot see that repository."
		;;
	401 | 403) fail "$2 returned $1 — the token was refused (expired, or without access to $REPO)." ;;
	000) fail "$2 could not be reached — check the network." ;;
	*) fail "$2 returned HTTP $1." ;;
	esac
}

# --- (6) resolve the release, ONCE -----------------------------------------
#
# THE ASSET URL CANNOT BE CONSTRUCTED FROM THE TAG. A private release's asset is
# fetched through the API asset URL (…/releases/assets/<id>), whose id lives only
# in the release object, so the release JSON is fetched once and each asset is
# selected from its assets[] BY NAME.
#
# STAGING SITS BESIDE THE DESTINATION, not in TMPDIR, so the placement at the end
# is an adjacent rename on one filesystem rather than a copy that can be
# interrupted half-written. The sweep clears orphans left by a run that died
# before its trap could fire; the `|| true` is required because an unmatched glob
# exits nonzero under `set -e`.
WORK="${HOME}/.knowledge/.staging-collector.$$"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM
rm -rf "$WORK"
rm -rf "${HOME}/.knowledge"/.staging-collector.* 2>/dev/null || true
mkdir -p "$WORK"

if [ -n "$PIN_VERSION" ]; then
	REL_URL="$GITHUB_API/repos/$REPO/releases/tags/$PIN_VERSION"
	REL_WHAT="the release for tag $PIN_VERSION"
else
	REL_URL="$GITHUB_API/repos/$REPO/releases/latest"
	REL_WHAT="the latest release"
fi
status="$(fetch "$REL_URL" "$WORK/release.json" "application/vnd.github+json")"
[ "$status" = "200" ] || refuse_fetch "$status" "$REL_WHAT"

TAG="$(tr -d ' \t\r\n' <"$WORK/release.json" | tr ',' '\n' |
	grep '^"tag_name":' | head -1 | sed 's/^"tag_name":"//;s/"$//')"
[ -n "$TAG" ] || fail "$REL_WHAT carried no tag_name."

# asset_url <asset name> — the API asset URL for one asset of this release,
# selected by name. Empty when the release does not carry it.
asset_url() {
	tr -d ' \t\r\n' <"$WORK/release.json" | tr '{' '\n' |
		grep -F "\"name\":\"$1\"" |
		grep -o '"url":"[^"]*/releases/assets/[0-9][0-9]*"' |
		head -1 | sed 's/^"url":"//;s/"$//'
}

ARCHIVE="${BIN_NAME}-${PLATFORM}.tar.gz"

# --- (7) the up-to-date check ----------------------------------------------
#
# It runs only when BOTH the binary and its sidecar are present, and it compares
# EXACTLY: a substring match reads an installed v1.2.30 as the target v1.2.3 and
# skips a download that was needed.
#
# THE SIDECAR IS NOT TRUSTED ON ITS OWN. The placed binary is asked for its own
# version and must agree; a binary its sidecar misdescribes is unknown state, and
# the answer is to reinstall it from a verified archive. The comparison reads the
# LAST FIELD OF THE FIRST LINE of `--version`, which is why that banner carries
# more than a bare token.
UP_TO_DATE=0
if [ -x "$DEST" ] && [ -f "$SIDECAR" ]; then
	sidecar_tag="$(head -1 "$SIDECAR" | tr -d ' \t\r')"
	binary_tag="$("$DEST" --version 2>/dev/null | head -1 | awk '{print $NF}')"
	if [ "$sidecar_tag" = "$TAG" ] && [ -n "$binary_tag" ] && [ "$binary_tag" = "$TAG" ]; then
		UP_TO_DATE=1
		echo "install.sh: $BIN_NAME is already at $TAG; nothing to download."
	elif [ "$sidecar_tag" = "$TAG" ] && [ "$binary_tag" != "$TAG" ]; then
		echo "install.sh: $BIN_NAME reports '$binary_tag' where its sidecar says '$sidecar_tag' — reinstalling." >&2
	fi
fi

# --- (8) download, verify, place -------------------------------------------
if [ "$UP_TO_DATE" -eq 0 ]; then
	archive_url="$(asset_url "$ARCHIVE")"
	sums_url="$(asset_url "checksums.txt")"
	[ -n "$archive_url" ] || fail "release $TAG carries no asset named $ARCHIVE."
	[ -n "$sums_url" ] || fail "release $TAG carries no checksums.txt — aborting, nothing installed."

	echo "install.sh: downloading $BIN_NAME $TAG for $PLATFORM"
	# application/octet-stream is what selects the asset's BYTES. Without it the
	# same URL answers with the asset's JSON metadata, which would be written to
	# disk as the "archive" and fail extraction with a confusing error.
	status="$(fetch "$archive_url" "$WORK/$ARCHIVE" "application/octet-stream")"
	[ "$status" = "200" ] || refuse_fetch "$status" "$ARCHIVE"
	status="$(fetch "$sums_url" "$WORK/checksums.txt" "application/octet-stream")"
	[ "$status" = "200" ] || refuse_fetch "$status" "checksums.txt"

	sha256_of() {
		if command -v sha256sum >/dev/null 2>&1; then
			sha256sum "$1" | awk '{print $1}'
		else
			shasum -a 256 "$1" | awk '{print $1}'
		fi
	}
	# A MISSING ENTRY ABORTS AS LOUDLY AS A MISMATCH. An archive nothing vouches
	# for is not installed on the grounds that its line was absent.
	want="$(awk -v a="$ARCHIVE" '{n=$2; sub(/^\*/,"",n)} n==a {print $1; exit}' "$WORK/checksums.txt")"
	[ -n "$want" ] || fail "checksums.txt has no entry for $ARCHIVE — aborting, nothing installed."
	got="$(sha256_of "$WORK/$ARCHIVE")"
	[ "$got" = "$want" ] || fail "sha256 mismatch for $ARCHIVE (expected $want, got $got) — aborting, nothing installed."

	# The archive holds ONE BARE BINARY at its root, named for the collector. It
	# is moved, never renamed: the name in the archive is the name in the entry.
	tar -xzf "$WORK/$ARCHIVE" -C "$WORK" || fail "extract failed: $ARCHIVE"
	[ -f "$WORK/$BIN_NAME" ] || fail "archive $ARCHIVE did not contain a '$BIN_NAME' binary — aborting, nothing installed."

	mkdir -p "$INSTALL_DIR"
	chmod 0755 "$WORK/$BIN_NAME"
	mv -f "$WORK/$BIN_NAME" "$DEST"
	printf '%s\n' "$TAG" >"$SIDECAR"
	echo "install.sh: installed $BIN_NAME $TAG to $INSTALL_DIR"

	placed_tag="$("$DEST" --version 2>/dev/null | head -1 | awk '{print $NF}')"
	if [ -n "$placed_tag" ] && [ "$placed_tag" != "$TAG" ]; then
		echo "install.sh: note — $BIN_NAME reports version '$placed_tag' while the release tag is '$TAG'." >&2
	fi
fi

# --- (9) register it -------------------------------------------------------
#
# The command is ABSOLUTE. The daemon resolves an entry's command once against
# its OWN PATH, which under a service manager holds a handful of system
# directories and not ~/.knowledge/bin, so a bare name would fail to resolve at
# the first collect.
#
# The add is byte-idempotent, so it runs on the up-to-date path too: an entry
# that was removed by hand is restored, and one that is already there is
# rewritten to the same bytes.
# refuse_userinfo <name> <value> — a literal that is a URL carrying userinfo is
# a credential, whatever class the name sits in.
#
# THE COMMON CASE IS A PROXY. HTTPS_PROXY and its siblings are selector-class on
# four of these collectors, and http://user:password@host:port is the standard
# way a corporate proxy password is carried. Writing that literal puts the
# password in a config file, which is the one thing this script never does.
#
# IT REFUSES RATHER THAN REWRITING. Stripping the userinfo would hand the
# collector a proxy URL that cannot authenticate, and substituting a reference
# would hand it an empty one on every collect; both are silent degrades of a
# value the operator set deliberately. Bad input errors.
#
# IT IS AN AUTHORITY TEST, NOT A SCHEME TEST, and that distinction is the whole
# guard. A proxy variable needs no scheme: Go's own proxy resolver retries a
# scheme-less value with http:// in front, so `user:pw@host:8080`,
# `//user:pw@host:8080` and `http://user:pw@host:8080` reach the same host with
# the same password on the wire, and every collector here is a Go HTTP client. A
# guard keyed on `://` sees only the third. The value is therefore SPLIT on the
# separators a multi-value proxy setting uses, each piece is normalised to an
# authority, and every authority in it is examined.
refuse_userinfo() {
	# Commas, semicolons and tabs are separators in these settings, the same as
	# spaces. The semicolon is here because a value can be written with either
	# one: the comma spelling was refused while its semicolon twin installed and
	# wrote the password.
	ru_list=$(printf '%s' "$2" | tr ',;\t' '   ')
	while [ -n "$ru_list" ]; do
		case "$ru_list" in
		' '*)
			ru_list="${ru_list# }"
			continue
			;;
		*' '*)
			ru_tok="${ru_list%% *}"
			ru_list="${ru_list#* }"
			;;
		*)
			ru_tok="$ru_list"
			ru_list=""
			;;
		esac
		[ -n "$ru_tok" ] || continue
		refuse_userinfo_token "$1" "$ru_tok"
	done
}

# refuse_userinfo_token — one whitespace-free piece of a value: normalise it to
# something with an authority, then refuse if ANY authority in it carries an @.
#
# THE LOOP IS NOT DECORATION: a value holding two URLs, the first with a path,
# hides the second one's userinfo from a scan that stops at the first authority.
refuse_userinfo_token() {
	ru_v="$2"
	case "$ru_v" in
	*://*) : ;;
	# No scheme: the whole piece IS the authority, which is what a Go client
	# makes of it. A leading `//` is the same value written the other way.
	*) ru_v="scheme://${ru_v#//}" ;;
	esac
	while :; do
		case "$ru_v" in
		*://*) ru_rest="${ru_v#*://}" ;;
		*) return 0 ;;
		esac
		case "${ru_rest%%/*}" in
		*@*)
			fail "$1 is a URL carrying userinfo (a credential), and this script writes no credential into the config entry. Remove the credentials from $1, or add that entry key yourself."
			;;
		esac
		ru_v="$ru_rest"
	done
}

set -- --tool "$TOOL"
env_rows="$(collector_env_table "$COLLECTOR")"
referenced_secrets=""
unset_secrets=""
while read -r class name; do
	[ -n "$name" ] || continue
	case "$name" in
	*[!A-Za-z0-9_]*) fail "the table row '$class $name' is not a variable name." ;;
	esac
	eval "value=\${$name:-}"
	case "$class" in
	path)
		# HOME is the one row written unconditionally: it was validated above and
		# without it no file-based credential chain resolves.
		if [ "$name" = "HOME" ]; then
			refuse_userinfo "HOME" "$HOME"
			set -- "$@" -e "HOME=$HOME"
		elif [ -n "$value" ]; then
			refuse_userinfo "$name" "$value"
			set -- "$@" -e "$name=$value"
		fi
		;;
	selector)
		if [ -n "$value" ]; then
			refuse_userinfo "$name" "$value"
			set -- "$@" -e "$name=$value"
		fi
		;;
	secret)
		# A SECRET NAME IS WRITTEN AS A BARE REFERENCE TO ITSELF, and its VALUE is
		# written nowhere. `"NAME": "${NAME}"` names the variable in the file and
		# leaves the credential in the operator's own environment, which is where
		# the process serving the collect reads it from at spawn.
		#
		# NOT THE VALUE, for the obvious reason. And NOT a `${NAME:-}` reference,
		# which is what this arm wrote two rounds ago: the default is expanded by
		# the process SERVING the collect, whose environment under a service
		# manager holds PATH and nothing else, so it reaches the collector as the
		# name PRESENT AND EMPTY — a credential that looks supplied and is not.
		# The bare form has no default to fall back to and is REFUSED BY NAME
		# instead, on the entry that carries it and on no other.
		#
		# THE KEY IS OMITTED ENTIRELY WHEN THIS SHELL DOES NOT HOLD THE NAME. A
		# reference to a name the operator never exported would refuse a collect
		# they never asked for: a collector's secret rows are commonly
		# alternatives — a token and its other spelling, a bearer token beside a
		# username and password — and only the ones they set are the ones they
		# authenticate with. It is the same set-ness gate the selector class uses,
		# and an operator who wants another name adds that one key themselves.
		if [ -n "$value" ]; then
			set -- "$@" -e "$name=\${$name}"
			referenced_secrets="$referenced_secrets $name"
		else
			unset_secrets="$unset_secrets $name"
		fi
		;;
	*) fail "the table row for $COLLECTOR carries an unknown class '$class'." ;;
	esac
done <<EOF
$env_rows
EOF

# NAMES ONLY, NEVER VALUES, in both reports. An operator is owed what was written
# for their credentials and what was not; printing the name costs nothing and
# printing the value is the thing this whole class exists to prevent.
if [ -n "$referenced_secrets" ]; then
	echo "install.sh: these credential names are written into the entry as \${NAME} references to your own environment, never as values:${referenced_secrets}"
	echo "install.sh: the value is read at collect time from the environment the daemon serves from — set it there, or the collect is refused by name."
fi
if [ -n "$unset_secrets" ]; then
	echo "install.sh: these credential names are not set in this shell, so no reference was written for them:${unset_secrets}"
	echo "install.sh: export the one your provider needs and re-run, or add that one key to the entry yourself."
fi

"$CLIENT" collector add "$@" "$COLLECTOR" -- "$DEST" ||
	fail "'knowledge collector add $COLLECTOR' failed — nothing was registered."

echo "install.sh: registered '$COLLECTOR' (tool $TOOL) at $DEST"
echo "install.sh: collect with: knowledge collect --type $COLLECTOR --id <graph>"
