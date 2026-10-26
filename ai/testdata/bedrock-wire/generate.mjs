// Regenerate: node --experimental-strip-types ai/testdata/bedrock-wire/generate.mjs ./pi > ai/testdata/bedrock-wire/golden.json
// Captures the pinned stream's command and AWS SDK HTTP request. No network.
import { registerHooks, createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root=resolve(process.argv[2]);
registerHooks({resolve(s,c,next){return s==="@earendil-works/pi-telemetry"?{url:pathToFileURL(resolve(root,"packages/telemetry/src/index.ts")).href,shortCircuit:true}:next(s,c);}});
const require=createRequire(resolve(root,"packages/ai/package.json"));
const {BedrockRuntimeClient}=require("@aws-sdk/client-bedrock-runtime");
const send=BedrockRuntimeClient.prototype.send;
let body,path;
BedrockRuntimeClient.prototype.send=function(command,options){
	this.config.requestHandler={handle:async request=>{
		if(body!==undefined)throw Error("unexpected second request");
		const bytes=request.body;
		body=typeof bytes==="string"?bytes:Buffer.from(bytes.buffer,bytes.byteOffset,bytes.byteLength).toString("utf8");path=request.path;
		throw Error("fixture-captured");
	}};
	return send.call(this,command,options);
};
const {stream}=await import(pathToFileURL(resolve(root,"packages/ai/src/api/bedrock-converse-stream.ts")));
const model={api:"bedrock-converse-stream",id:"anthropic.claude-sonnet-4-5-20250929-v1:0",name:"Claude Sonnet 4.5",provider:"amazon-bedrock",baseUrl:"https://invalid.test",
	reasoning:true,maxTokens:8192,contextWindow:200000,input:["text","image"],compat:{supportsStrictMode:true},cost:{input:3,output:15,cacheRead:0.3,cacheWrite:3.75}};
const cases=[{name:"minimal",messages:[{role:"user",content:"hello",timestamp:1}],options:{}}, {
	name:"request-metadata-order",messages:[{role:"user",content:"hello",timestamp:1}],options:{requestMetadata:{z:"1",a:"2"}},
}, {
	name:"configured-tools",messages:[
		{role:"system",content:"system",toolsAdded:[{name:"lookup",description:"Look up",parameters:{type:"object",properties:{z:{type:"string"},a:{type:"number"}},required:["z","a"]},constrainedSampling:{type:"json_schema",strict:"prefer"}}],timestamp:1},
		{role:"user",content:"hello",timestamp:2},
		{role:"assistant",content:[{type:"toolCall",id:"call-1",name:"lookup",arguments:{z:1,a:2,"":3,nested:{z:1,"":2,a:3}}}],stopReason:"toolUse",timestamp:3},
		{role:"toolResult",toolCallId:"call-1",toolName:"lookup",content:[{type:"text",text:"found"}],isError:false,timestamp:4},
	],options:{temperature:0,maxTokens:16,toolChoice:"any",cacheRetention:"long",requestMetadata:{team:"test"}},
}, {
	name:"thinking",messages:[{role:"user",content:"hello",timestamp:1},
		{role:"assistant",content:[{type:"thinking",thinking:"think",thinkingSignature:"sig"},{type:"text",text:"answer"}],stopReason:"stop",timestamp:2},
		{role:"user",content:"continue",timestamp:3}],options:{reasoning:"low"},
}, {
	name:"payload-replacement",messages:[{role:"user",content:"hello",timestamp:1}],options:{},
	replacement:{modelId:"custom:model",messages:[{role:"user",content:[{text:"changed"}]}],
		inferenceConfig:{topP:0.5,maxTokens:7,temperature:0,stopSequences:["END"],ignored:"ignore"},
		additionalModelRequestFields:{z:1,a:2},requestMetadata:{z:"1",a:"2"},extra:{z:1}},
}, {
	name:"binary-content",messages:[
		{role:"user",content:[{type:"text",text:"look"},{type:"image",data:"aW1hZ2U=",mimeType:"image/png"}],timestamp:1},
		{role:"assistant",content:[{type:"thinking",thinking:"",thinkingSignature:"c2ln",redacted:true}],stopReason:"stop",timestamp:2},
		{role:"user",content:"continue",timestamp:3},
	],options:{},
}, {
	name:"nova-defaults",modelId:"amazon.nova-pro-v1:0",modelName:"Nova Pro",messages:[{role:"user",content:"hello",timestamp:1}],options:{},
}, {
	name:"adaptive-thinking",modelId:"anthropic.claude-opus-4-7-v1:0",modelName:"Claude Opus 4.7",messages:[
		{role:"system",content:"system",toolsAdded:[{name:"empty-key",description:"Empty key is valid",parameters:{type:"object",properties:{"":{type:"string"},z:{type:"number"}},required:[]}}],timestamp:1},
		{role:"user",content:"hello",timestamp:2}],options:{reasoning:"medium",thinkingDisplay:"omitted",cacheRetention:"none"},
}, {
	name:"required-strict-unsupported",expectError:true,strictSupport:false,
	messages:[{role:"system",content:"system",toolsAdded:[{name:"lookup",parameters:{type:"object",properties:{}},constrainedSampling:{type:"json_schema",strict:"require"}}],timestamp:1},{role:"user",content:"hello",timestamp:2}],options:{},
}, {
	name:"required-strict-schema-error",expectError:true,
	messages:[{role:"system",content:"system",toolsAdded:[{name:"lookup",parameters:{type:"object",$ref:"#/$defs/X"},constrainedSampling:{type:"json_schema",strict:"require"}}],timestamp:1},{role:"user",content:"hello",timestamp:2}],options:{},
}, {
	name:"tool-result-image-error",messages:[
		{role:"user",content:"shot",timestamp:1},
		{role:"assistant",content:[{type:"toolCall",id:"call-2",name:"shot",arguments:{z:"x"}}],stopReason:"toolUse",timestamp:2},
		{role:"toolResult",toolCallId:"call-2",toolName:"shot",content:[{type:"text",text:"see"},{type:"image",data:"aW1n",mimeType:"image/jpeg"}],isError:true,timestamp:3},
	],options:{},
}, {
	name:"tools-removed",messages:[
		{role:"system",content:"sys",toolsRemoved:[{name:"old"},{name:"older"}],timestamp:1},
		{role:"user",content:"hello",timestamp:2},
	],options:{toolChoice:"none"},
}, {
	name:"assistant-multi-text",messages:[
		{role:"user",content:"go",timestamp:1},
		{role:"assistant",content:[{type:"text",text:"one"},{type:"text",text:"two"},{type:"thinking",thinking:"plan",thinkingSignature:"sig"},{type:"text",text:"three"}],stopReason:"stop",timestamp:2},
		{role:"user",content:"again",timestamp:3},
	],options:{},
}, {
	name:"empty-user-text",messages:[
		{role:"user",content:"",timestamp:1},
		{role:"assistant",content:[{type:"text",text:""}],stopReason:"stop",timestamp:2},
		{role:"user",content:"real",timestamp:3},
	],options:{},
}, {
	name:"disabled-required-strict",strictSupport:false,
	messages:[{role:"system",content:"system",toolsAdded:[{name:"lookup",parameters:{type:"object",$ref:"#/$defs/X"},constrainedSampling:{type:"json_schema",strict:"require"}}],timestamp:1},{role:"user",content:"hello",timestamp:2}],options:{toolChoice:"none"},
}];
const fixtures=[];
for(const input of cases){
	body=path=undefined;let payload;
	const activeModel={...model,id:input.modelId??model.id,name:input.modelName??model.name,compat:{supportsStrictMode:input.strictSupport??true}};
	const messages=input.messages.map(message=>message.role==="assistant"?{...message,api:activeModel.api,provider:activeModel.provider,model:activeModel.id}:message);
	const result=await stream(activeModel,{messages},{...input.options,env:{AWS_BEDROCK_SKIP_AUTH:"1"},maxRetries:0,onPayload(p){
		// D268: Go's JSON hook represents native Uint8Array values as base64.
		// Normalize only the observed hook value, never the SDK command/body.
		payload=JSON.stringify(p,(_key,value)=>value instanceof Uint8Array?Buffer.from(value.buffer,value.byteOffset,value.byteLength).toString("base64"):value);
		return input.replacement;
	}}).result();
	if(input.expectError){
		if(body!==undefined||payload!==undefined||!result.errorMessage)throw Error("strict requirement did not fail before hooks/HTTP");
		fixtures.push({...input,messages,error:result.errorMessage});continue;
	}
	if(body===undefined||result.errorMessage!=="fixture-captured")throw Error(result.errorMessage??"no capture");
	fixtures.push({...input,messages,payload,path,body});
}
console.log(JSON.stringify(fixtures,null,2));
