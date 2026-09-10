#!/bin/sh
# uninstall.sh — reverse what install.sh did for one collector.
#
#   sh uninstall.sh aws
#
# Removes the collector binary and its version sidecar from ~/.knowledge/bin and
# removes the config entry with `knowledge collector remove`.
#
# IT TOUCHES NO GRAPH. Removing a collector unregisters the provider; the nodes
# a past collect wrote are data, and deleting them is a separate, deliberate act
# an operator performs with the client.

set -eu

INSTALL_DIR="${HOME:-}/.knowledge/bin"
COLLECTORS="aws azure bitbucket-pipelines cloudwatch gcp github-actions gitlab-ci k8s k8s-logs loki stackdriver"

fail() {
	echo "uninstall.sh: $*" >&2
	exit 1
}

COLLECTOR=""
while [ $# -gt 0 ]; do
	case "$1" in
	--help | -h)
		echo "usage: uninstall.sh <collector>"
		echo "collectors: $COLLECTORS"
		exit 0
		;;
	-*) fail "unknown flag: $1" ;;
	*)
		[ -z "$COLLECTOR" ] || fail "one collector at a time: already given '$COLLECTOR', then '$1'"
		COLLECTOR="$1"
		;;
	esac
	shift
done

[ -n "$COLLECTOR" ] || fail "which collector? name one of: $COLLECTORS"
known=0
for c in $COLLECTORS; do
	# `[ ... ] && known=1` would be a set -e trap here: the && list exits
	# non-zero on every collector that is not the one named, and that failure is
	# the script's own.
	if [ "$c" = "$COLLECTOR" ]; then
		known=1
	fi
done
[ "$known" -eq 1 ] || fail "unknown collector '$COLLECTOR' — known collectors: $COLLECTORS"

[ -n "${HOME:-}" ] || fail "HOME is unset; it is where collectors are installed."

BIN_NAME="knowledge-collector-${COLLECTOR}"
DEST="$INSTALL_DIR/$BIN_NAME"
SIDECAR="$DEST.version"

removed_files=0
if [ -e "$DEST" ]; then
	rm -f "$DEST"
	removed_files=1
fi
if [ -e "$SIDECAR" ]; then
	rm -f "$SIDECAR"
	removed_files=1
fi
if [ "$removed_files" -eq 1 ]; then
	echo "uninstall.sh: removed $DEST and its version sidecar"
else
	echo "uninstall.sh: no $BIN_NAME binary in $INSTALL_DIR"
fi

if [ -x "$INSTALL_DIR/knowledge" ]; then
	CLIENT="$INSTALL_DIR/knowledge"
elif command -v knowledge >/dev/null 2>&1; then
	CLIENT="$(command -v knowledge)"
else
	fail "the knowledge client is not on PATH, so the config entry cannot be removed — the binary is gone; run \`knowledge collector remove $COLLECTOR\` when the client is back."
fi

# THE REMOVE VERB IS DELIBERATELY NOT IDEMPOTENT: removing an entry that is not
# there exits non-zero naming the file. That one failure is tolerated, because a
# second uninstall of the same collector is an ordinary thing to run — and it is
# matched BY ITS MESSAGE rather than by the exit status alone, so a remove that
# failed for any other reason (an unreadable file, a bad scope) is still reported
# and still fails this script.
set +e
remove_out="$("$CLIENT" collector remove "$COLLECTOR" 2>&1)"
remove_rc=$?
set -e
if [ "$remove_rc" -eq 0 ]; then
	echo "uninstall.sh: removed the '$COLLECTOR' config entry"
	exit 0
fi
case "$remove_out" in
*"is not in this file"*)
	echo "uninstall.sh: no '$COLLECTOR' config entry to remove"
	exit 0
	;;
esac
echo "$remove_out" >&2
fail "'knowledge collector remove $COLLECTOR' failed."
