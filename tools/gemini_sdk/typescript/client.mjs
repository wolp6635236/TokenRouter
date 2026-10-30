import { GoogleGenAI } from '@google/genai';

// client.mjs validates local gateway streams with Google's TypeScript SDK package.
// The route fixture controls upstream responses and synthetic authentication;
// this executable checks the actual decoder without adding the SDK to the
// production frontend or backend dependency graph.

// main consumes the selected fixture and checks its complete stream outcome.
// Arguments select the gateway and expected result, with optional model and
// prompt overrides reserved for the explicitly enabled real-upstream fixture.
async function main() {
  const [baseUrl, expected, model = 'gemini-2.5-flash', prompt = 'Reply with exactly OK.'] = process.argv.slice(2);
  const client = new GoogleGenAI({
    apiKey: 'fixture-key',
    httpOptions: { baseUrl, apiVersion: 'v1beta', timeout: 180000 },
  });
  let chunks = 0;
  let empty = 0;
  let text = '';
  let failure = null;
  let errorCode = 0;
  let totalTokens = 0;
  try {
    const stream = await client.models.generateContentStream({
      model,
      contents: prompt,
      config: { maxOutputTokens: 4096, thinkingConfig: { thinkingLevel: 'HIGH' } },
    });
    for await (const response of stream) {
      chunks++;
      if (response.usageMetadata) totalTokens = response.usageMetadata.totalTokenCount ?? 0;
      if (!response.candidates?.length && !response.usageMetadata && !response.promptFeedback && !response.responseId && !response.modelVersion && !response.modelStatus && !response.createTime) {
        empty++;
      }
      for (const candidate of response.candidates ?? []) {
        for (const part of candidate.content?.parts ?? []) {
          if (!part.thought) text += part.text ?? '';
        }
      }
    }
  } catch (error) {
    failure = String(error);
    if (error?.name === 'ApiError' && typeof error.status === 'number') errorCode = error.status;
  }
  const ok = empty === 0 && (expected === 'error' ? errorCode >= 400 : failure === null && text.length > 0);
  console.log(JSON.stringify({ ok, chunks, empty_chunks: empty, text_bytes: Buffer.byteLength(text), error: failure, error_code: errorCode, total_tokens: totalTokens }));
  if (!ok) process.exitCode = 1;
}

await main();
