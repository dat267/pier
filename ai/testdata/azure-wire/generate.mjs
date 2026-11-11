// Regenerate: node --experimental-strip-types ai/testdata/azure-wire/generate.mjs ./pi > ai/testdata/azure-wire/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2]);
registerHooks({ resolve(s,c,next) { return s === "@earendil-works/pi-telemetry"
	? { url: pathToFileURL(resolve(root,"packages/telemetry/src/index.ts")).href, shortCircuit: true } : next(s,c); } });
const { stream } = await import(pathToFileURL(resolve(root,"packages/ai/src/api/azure-openai-responses.ts")));
const cases = [{ name: "minimal", messages: [{role:"user",content:"hello",timestamp:1}], options: {} }, {
	name:"endpoint-options",messages:[{role:"user",content:"hello",timestamp:1}],
	options:{azureBaseUrl:"https://invalid.test/custom",azureApiVersion:"2026-01-01",azureDeploymentName:"deployment"},
}, {
	name:"tools-and-generation",messages:[
		{role:"system",content:"system",toolsAdded:[{name:"lookup",description:"Look up",parameters:{type:"object",properties:{z:{type:"string"},a:{type:"number"}},required:["z","a"]},constrainedSampling:{type:"json_schema",strict:"prefer"}}],timestamp:1},
		{role:"user",content:[{type:"text",text:"look"},{type:"image",data:"aW1hZ2U=",mimeType:"image/png"}],timestamp:2},
	], options:{temperature:0,maxTokens:1,toolChoice:"auto",reasoningEffort:"low",reasoningSummary:"auto",cacheRetention:"none",sessionId:"session"},
}, {
	name:"long-cache",messages:[{role:"user",content:"hello",timestamp:1}],options:{cacheRetention:"long",sessionId:"session"},
}, {
	name:"payload-replacement",messages:[{role:"user",content:"hello",timestamp:1}],options:{},
	replacement:{model:"replacement",input:[{role:"user",content:"changed"}],stream:true,metadata:{z:"1",a:"2"},custom:{z:1,a:2}},
},
{ name:"reasoning-replay", messages:[
	{role:"user",content:"go",timestamp:1},
	{role:"assistant",timestamp:2,stopReason:"toolUse",content:[
		{type:"thinking",thinking:"plan",thinkingSignature:"c2ln"},
		{type:"thinking",thinking:"[Reasoning redacted]",thinkingSignature:"cmVk",redacted:true},
		{type:"text",text:"calling"},
		{type:"toolCall",id:"call_1",name:"lookup",arguments:{q:"x"}}]},
	{role:"toolResult",toolCallId:"call_1",toolName:"lookup",isError:false,content:[{type:"text",text:"result"}],timestamp:3},
	{role:"user",content:"next",timestamp:4},
], options:{} },
{ name:"parallel-tools-key-order", messages:[
	{role:"user",content:"both",timestamp:1},
	{role:"assistant",timestamp:2,stopReason:"toolUse",content:[
		{type:"toolCall",id:"call_a",name:"lookup",arguments:{zeta:1,alpha:[null,true,false,1.5,"x"],mid:{nested:[],"ключ":"välue"},path:"a"}},
		{type:"toolCall",id:"call_b",name:"lookup",arguments:{}}]},
	{role:"toolResult",toolCallId:"call_a",toolName:"lookup",isError:false,content:[{type:"text",text:"A"}],timestamp:3},
	{role:"toolResult",toolCallId:"call_b",toolName:"lookup",isError:true,content:[{type:"text",text:"B"}],timestamp:4},
], options:{} },
{ name:"two-system-and-images", messages:[
	{role:"system",content:"first system",timestamp:0},
	{role:"system",content:"second system",timestamp:1},
	{role:"user",content:[{type:"text",text:"two shots"},{type:"image",data:"aW1hZ2Ux",mimeType:"image/png"},{type:"image",data:"aW1hZ2Uy",mimeType:"image/gif"}],timestamp:2},
], options:{} },
{ name:"unicode-and-escapes", messages:[
	{role:"system",content:'sys é日 "quoted" \\backslash\nnewline',timestamp:0},
	{role:"user",content:"emoji 😀🚀 astral 𝒜 cjk 日本語\nsecond line",timestamp:1},
], options:{temperature:0.25,maxTokens:64} },
{ name:"empty-text", messages:[
	{role:"user",content:[{type:"text",text:""},{type:"text",text:"   "},{type:"text",text:"real"}],timestamp:1},
	{role:"assistant",timestamp:2,stopReason:"stop",content:[{type:"text",text:""}]},
	{role:"user",content:"again",timestamp:3},
], options:{} },
];
const model = { api:"azure-openai-responses", id:"gpt-5", provider:"azure", baseUrl:"https://invalid.test/openai/v1", reasoning:true,
	maxTokens:8192, contextWindow:128000, input:["text","image"], cost:{input:0,output:0,cacheRead:0,cacheWrite:0} };
const fixtures=[];
for(const input of cases) {
	let payload,body,path,apiVersion;
	globalThis.fetch=async (url,init)=>{
		const parsed=new URL(url); path=parsed.pathname;apiVersion=parsed.searchParams.get("api-version");body=init.body;
		return new Response('data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}\n\n', {headers:{"content-type":"text/event-stream"}});
	};
	const result=await stream(model,{messages:input.messages},{...input.options,apiKey:"token",env:{},onPayload(p){payload=JSON.stringify(p);return input.replacement;}}).result();
	if(result.errorMessage||typeof body!=="string")throw Error(result.errorMessage??"no request");
	fixtures.push({...input,payload,path,apiVersion,body});
}
console.log(JSON.stringify(fixtures,null,2));
