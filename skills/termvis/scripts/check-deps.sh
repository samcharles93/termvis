#!/usr/bin/env bash
# Preflight check for termvis's runtime dependencies: ttyd, a Chrome/Chromium-
# based browser, and the termvis binary itself. Run this before debugging a
# confusing "Error starting ttyd" / "Error launching browser" failure — it's
# almost always one of these missing, not a bug in a script.
set -uo pipefail

ok=true

if command -v ttyd >/dev/null 2>&1; then
	echo "ok: ttyd found at $(command -v ttyd)"
else
	echo "missing: ttyd — install it (e.g. 'brew install ttyd' on macOS, or see https://github.com/tsl0922/ttyd#installation)"
	ok=false
fi

browser=""
for candidate in google-chrome google-chrome-stable chromium chromium-browser microsoft-edge; do
	if command -v "$candidate" >/dev/null 2>&1; then
		browser="$candidate"
		break
	fi
done

if [ -n "$browser" ]; then
	echo "ok: Chrome/Chromium-based browser found: $(command -v "$browser")"
else
	echo "missing: no Chrome/Chromium-based browser on PATH (checked: google-chrome, google-chrome-stable, chromium, chromium-browser, microsoft-edge)"
	ok=false
fi

if command -v termvis >/dev/null 2>&1; then
	echo "ok: termvis found at $(command -v termvis)"
else
	echo "note: termvis not on PATH — set TERMVIS_PATH, or run 'go install github.com/samcharles93/termvis@latest'"
fi

echo
if [ "$ok" = false ]; then
	echo "termvis will fail to start until the missing dependencies above are installed."
	exit 1
fi

echo "All required dependencies are present."
