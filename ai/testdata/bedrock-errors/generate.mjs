// Regenerate: node --experimental-strip-types ai/testdata/bedrock-errors/generate.mjs ./pi > ai/testdata/bedrock-errors/golden.json
// Captures AWS SDK errors as observed by pi, without network access.
import {registerHooks, createRequire} from "node:module";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {Readable} from "node:stream";

const root = resolve(process.argv[2]);
registerHooks({
	resolve(specifier, context, nextResolve) {
		if (specifier === "@earendil-works/pi-telemetry") {
			return {url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true};
		}
		return nextResolve(specifier, context);
	},
});
const require = createRequire(resolve(root, "packages/ai/package.json"));
const sdk = require("@aws-sdk/client-bedrock-runtime");
const {BedrockRuntimeClient} = sdk;
const originalSend = BedrockRuntimeClient.prototype.send;
let scenario;
BedrockRuntimeClient.prototype.send = function(command, options) {
	if (scenario.streamError) {
		const Exception = sdk[scenario.streamError.name];
		const exception = new Exception({
			message: scenario.streamError.message,
			originalStatusCode: scenario.streamError.originalStatusCode,
			originalMessage: scenario.streamError.originalMessage,
			$metadata: {httpStatusCode: 200, requestId: scenario.requestId},
		});
		return Promise.resolve({
			$metadata: {httpStatusCode: 200, requestId: scenario.requestId},
			stream: (async function*() { yield {[scenario.streamError.member]: exception}; })(),
		});
	}
	this.config.requestHandler = {
		handle: async () => ({response: {
			statusCode: scenario.http.status,
			headers: {
				"content-type": scenario.http.contentType,
				"x-amzn-requestid": scenario.requestId,
			},
			body: Readable.from([Buffer.from(scenario.http.body)]),
		}}),
	};
	return originalSend.call(this, command, options);
};

const {stream} = await import(pathToFileURL(resolve(root, "packages/ai/src/api/bedrock-converse-stream.ts")));
const model = {
	api: "bedrock-converse-stream", id: "anthropic.claude-sonnet-4-5-20250929-v1:0", name: "Claude Sonnet 4.5",
	provider: "amazon-bedrock", baseUrl: "https://invalid.test", reasoning: true, maxTokens: 8192,
	contextWindow: 200000, input: ["text", "image"], compat: {supportsStrictMode: true},
	cost: {input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75},
};
const cases = [
	{name: "modeled-throttling-json", requestId: "req-throttle", http: {status: 429, contentType: "application/json", body: '{"message":"rate exceeded","__type":"ThrottlingException"}'}},
	{name: "gateway-json-error-object", requestId: "req-json", http: {status: 403, contentType: "application/json", body: '{"error":{"message":"gateway says no"}}'}},
	{name: "gateway-json-message", requestId: "req-message", http: {status: 403, contentType: "application/json", body: '{"message":"no access"}'}},
	{name: "gateway-json-scalar", requestId: "req-scalar", http: {status: 418, contentType: "application/json", body: '"gateway says no"'}},
	{name: "gateway-text-body", requestId: "req-text", dRow: "D270", http: {status: 502, contentType: "text/plain", body: "proxy exploded"}},
	{name: "empty-http-body", requestId: "req-empty", http: {status: 400, contentType: "application/json", body: ""}},
	{name: "modeled-stream-error", requestId: "req-stream", streamError: {name: "ModelStreamErrorException", member: "modelStreamErrorException", message: "model said no", originalStatusCode: 503, originalMessage: "backend unavailable"}},
	{name: "overflow-validation-exception", requestId: "req-overflow", http: {status: 400, contentType: "application/json", body: '{"message":"Input is too long for requested model.","__type":"ValidationException"}'}},
	{name: "namespaced-error-type", requestId: "req-ns", http: {status: 400, contentType: "application/json", body: '{"message":"model error","__type":"com.amazon#ModelErrorException"}'}},
	{name: "throttling-stream-error", requestId: "req-stream-throttle", streamError: {name: "ThrottlingException", member: "throttlingException", message: "slow down", originalStatusCode: 429, originalMessage: "rate exceeded"}},
	{name: "non-string-message", requestId: "req-nonstring", http: {status: 400, contentType: "application/json", body: '{"message":123,"__type":"ValidationException"}'}},
	{name: "json-array-body", requestId: "req-array", http: {status: 500, contentType: "application/json", body: '[1,2,3]'}},
	{name: "unicode-message", requestId: "req-unicode", http: {status: 400, contentType: "application/json", body: '{"message":"größe überschritten 😀","__type":"ValidationException"}'}},
	{name: "boolean-message", requestId: "req-bool", http: {status: 400, contentType: "application/json", body: '{"message":true,"__type":"ValidationException"}'}},
	{name: "null-message", requestId: "req-null", http: {status: 400, contentType: "application/json", body: '{"message":null,"__type":"ValidationException"}'}},
	{name: "object-message", requestId: "req-object", http: {status: 400, contentType: "application/json", body: '{"message":{"a":1},"__type":"ValidationException"}'}},
	{name: "array-message", requestId: "req-array-msg", http: {status: 400, contentType: "application/json", body: '{"message":["x","y"],"__type":"ValidationException"}'}},
	{name: "float-message", requestId: "req-float", http: {status: 400, contentType: "application/json", body: '{"message":1.5,"__type":"ValidationException"}'}},
	{name: "empty-stream-exception-message", requestId: "req-empty-stream", streamError: {name: "ThrottlingException", member: "throttlingException", message: "", originalStatusCode: 429, originalMessage: ""}},
];
const fixtures = [];
for (scenario of cases) {
	const result = await stream(model, {messages: [{role: "user", content: "hello", timestamp: 1}]}, {
		env: {AWS_BEDROCK_SKIP_AUTH: "1"}, maxRetries: 0,
	}).result();
	fixtures.push({
		...scenario,
		errorMessage: result.errorMessage,
		stopReason: result.stopReason,
		diagnosticDetails: result.diagnostics?.[0]?.details,
	});
}
console.log(JSON.stringify(fixtures, null, 2));
