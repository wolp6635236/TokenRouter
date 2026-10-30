import json
import sys

from google import genai
from google.genai import errors, types

# client.py validates local gateway streams with the isolated Google Python SDK.
# The owning route fixture supplies synthetic authentication and controls upstream
# behavior, while this process checks that heartbeats remain invisible and stream
# failures reach the real SDK's ordinary exception path.


def main():
    """Consume a complete SDK stream and validate its expected outcome.

    Arguments select the local gateway, success or error, and optional model and
    prompt. The client closes its transport after every stream, including failures.
    """
    model = sys.argv[3] if len(sys.argv) > 3 else "gemini-2.5-flash"
    prompt = sys.argv[4] if len(sys.argv) > 4 else "Reply with exactly OK."
    chunks = 0
    empty = 0
    text = ""
    failure = None
    error_code = 0
    total_tokens = 0
    with genai.Client(
        api_key="fixture-key",
        http_options=types.HttpOptions(
            base_url=sys.argv[1], api_version="v1beta", timeout=180000
        ),
    ) as client:
        try:
            for response in client.models.generate_content_stream(
                model=model,
                contents=prompt,
                config=types.GenerateContentConfig(
                    max_output_tokens=4096,
                    thinking_config=types.ThinkingConfig(thinking_level="HIGH"),
                ),
            ):
                chunks += 1
                if response.usage_metadata:
                    total_tokens = response.usage_metadata.total_token_count or 0
                if not any(
                    (
                        response.candidates,
                        response.usage_metadata,
                        response.prompt_feedback,
                        response.response_id,
                        response.model_version,
                        response.model_status,
                        response.create_time,
                    )
                ):
                    empty += 1
                for candidate in response.candidates or []:
                    for part in candidate.content.parts if candidate.content else []:
                        if not part.thought:
                            text += part.text or ""
        except Exception as error:
            failure = str(error)
            if isinstance(error, errors.APIError):
                error_code = error.code or 0
    want_error = sys.argv[2] == "error"
    ok = empty == 0 and (
        (want_error and error_code >= 400)
        or (not want_error and failure is None and len(text) > 0)
    )
    print(json.dumps({"ok": ok, "chunks": chunks, "empty_chunks": empty, "text_bytes": len(text.encode()), "error": failure, "error_code": error_code, "total_tokens": total_tokens}))
    if not ok:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
