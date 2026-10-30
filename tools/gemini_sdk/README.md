# Google SDK stream checks

These clients call the registered local gateway routes from `internal/app` tests.
They verify that empty heartbeats produce no SDK chunks, successful responses
retain text and usage, and failed streams raise API errors rather than decoder
or transport errors. The SDK dependencies are isolated from production modules.

The pinned clients are Go 1.76.0, Python 2.34.0, and TypeScript 2.32.0. Use the
repository Go toolchain, Python 3.10 or newer, and a Node version supported by the
TypeScript package. Run the following from the repository root:

```sh
go -C tools/gemini_sdk/go mod download
python3.14 -m venv /tmp/tokenrouter-gemini-sdk-venv
/tmp/tokenrouter-gemini-sdk-venv/bin/pip install -r tools/gemini_sdk/python/requirements.txt
npm ci --prefix tools/gemini_sdk/typescript --ignore-scripts
TOKENROUTER_GEMINI_SDK_TESTS=1 TOKENROUTER_GEMINI_SDK_PYTHON=/tmp/tokenrouter-gemini-sdk-venv/bin/python go -C backend test ./internal/app -run '^TestGeminiSDKRoutes$' -count=1 -v
```

The controlled long-wait check uses the Go client and waits 130 seconds before
releasing the upstream payload:

```sh
TOKENROUTER_GEMINI_SDK_LONG_WAIT=1 go -C backend test ./internal/app -run '^TestGeminiSDKLongWait$' -count=1 -v
```

`TestGeminiSDKLive` makes two real requests. Enable it only for an authorized
provider by setting `TOKENROUTER_GEMINI_SDK_LIVE=1`,
`TOKENROUTER_GEMINI_LIVE_BASE_URL`, and `TOKENROUTER_GEMINI_LIVE_KEY` through local
credential tooling. It uses `gemini-4-argon`; the key stays in the local upstream
proxy and is not passed to SDK processes or stored in fixture provider records.

All three opt-in suites are skipped during ordinary unit tests. The shared
reader, HTTP writer, and route regressions run without SDK installation or live
provider credentials.
