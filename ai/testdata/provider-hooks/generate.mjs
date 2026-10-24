// Regenerate: node --experimental-strip-types ai/testdata/provider-hooks/generate.mjs ./pi > ai/testdata/provider-hooks/golden.json
import {registerHooks} from "node:module";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {zstdDecompressSync} from "node:zlib";
const root=resolve(process.argv[2]);
registerHooks({resolve(s,c,next){return s==="@earendil-works/pi-telemetry"?{url:pathToFileURL(resolve(root,"packages/telemetry/src/index.ts")).href,shortCircuit:true}:next(s,c);}});
const metadata={z:"1",a:"2"},custom={z:1,a:2};
const toolMessage={role:"system",content:"system",toolsAdded:[{name:"lookup",description:"Look up",parameters:{type:"object",properties:{z:{type:"string"},a:{type:"number"}},required:["z","a"]}}],timestamp:1};
const cases=[
	{name:"openai-completions-replacement",api:"openai-completions",id:"gpt-4o",provider:"openai",baseUrl:"https://invalid.test/v1",messages:[{role:"user",content:"hello",timestamp:1}],replacement:{model:"replacement",messages:[{role:"user",content:"changed"}],stream:true,metadata,custom}},
	{name:"anthropic-messages-replacement",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[toolMessage,{role:"user",content:"hello",timestamp:2}],replacement:{model:"replacement",messages:[{role:"user",content:"changed"}],max_tokens:8,stream:false,betas:["custom-beta","custom-two"],metadata,custom}},
	{name:"anthropic-messages-default",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[toolMessage,{role:"user",content:"hello",timestamp:2}]},
	{name:"anthropic-messages-beta-header",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[toolMessage,{role:"user",content:"hello",timestamp:2}],compat:{supportsEagerToolInputStreaming:false}},
	{name:"anthropic-messages-oauth",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[toolMessage,{role:"user",content:"hello",timestamp:2}],oauth:true},
	{name:"openai-completions-error",api:"openai-completions",id:"gpt-4o",provider:"openai",baseUrl:"https://invalid.test/v1",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:401,body:'{"error":{"message":"no key"}}'}},
	{name:"openai-responses-error",api:"openai-responses",id:"gpt-5",provider:"openai",baseUrl:"https://invalid.test/v1",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:403,body:"nope"}},
	{name:"anthropic-messages-error",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:401,body:'{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}'}},
	{name:"anthropic-messages-text-error",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:403,body:"nope"}},
	{name:"anthropic-messages-empty-error",api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:400,body:""}},
	{name:"google-generative-ai-json-error",api:"google-generative-ai",id:"gemini-3-pro",provider:"google",baseUrl:"https://invalid.test/v1beta",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:401,statusText:"Unauthorized",contentType:"application/json",body:'{"error":{"code":401,"message":"API key not valid"}}'}},
	{name:"google-generative-ai-nested-error",api:"google-generative-ai",id:"gemini-3-pro",provider:"google",baseUrl:"https://invalid.test/v1beta",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:403,statusText:"Forbidden",contentType:"application/json; charset=utf-8",body:'{"z":1,"a":{"y":2,"b":3}}'}},
	{name:"google-generative-ai-text-error",api:"google-generative-ai",id:"gemini-3-pro",provider:"google",baseUrl:"https://invalid.test/v1beta",messages:[{role:"user",content:"hello",timestamp:1}],fail:{status:403,statusText:"Forbidden",contentType:"text/plain",body:"nope"}},
	{name:"openai-codex-responses-replacement",api:"openai-codex-responses",id:"gpt-5-codex",provider:"openai-codex",baseUrl:"https://invalid.test/v1",messages:[{role:"user",content:"hello",timestamp:1}],replacement:{model:"replacement",input:[{role:"user",content:"changed"}],stream:true,store:false,metadata,custom}},
];
const fixtures=[];
for(const input of cases){
	const {stream}=await import(pathToFileURL(resolve(root,`packages/ai/src/api/${input.api}.ts`)));
	let body,path,payload,httpHeaders;
	globalThis.fetch=async(url,init)=>{
		path=new URL(url).pathname;
		const headers=new Headers(init.headers);httpHeaders={};
		for(const name of ["anthropic-beta","originator","openai-beta","session_id"]){const value=headers.get(name);if(value!==null)httpHeaders[name]=value;}
		body=headers.get("content-encoding")==="zstd"?zstdDecompressSync(init.body).toString("utf8"):init.body;
		if(input.fail)return new Response(input.fail.body,{status:input.fail.status,statusText:input.fail.statusText??"",headers:{"content-type":input.fail.contentType??"application/json"}});
		let event='data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}\n\n';
		if(input.api==="openai-completions")event='data: {"id":"r","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n';
		if(input.api==="anthropic-messages")event='event: message_start\ndata: {"type":"message_start","message":{"id":"r","model":"claude-sonnet-4-5","usage":{"input_tokens":1,"output_tokens":0}}}\n\nevent: message_delta\ndata: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}\n\nevent: message_stop\ndata: {"type":"message_stop"}\n\n';
		return new Response(event,{headers:{"content-type":"text/event-stream"}});
	};
	const model={api:input.api,id:input.id,provider:input.provider,baseUrl:input.baseUrl,reasoning:false,maxTokens:8192,contextWindow:128000,input:["text","image"],cost:{input:0,output:0,cacheRead:0,cacheWrite:0}};
	if(input.compat)model.compat=input.compat;
	const messages=input.messages;
	const apiKey=input.oauth?"sk-ant-oat01-token":input.api==="openai-codex-responses"?`h.${Buffer.from(JSON.stringify({"https://api.openai.com/auth":{chatgpt_account_id:"acct"}})).toString("base64url")}.s`:"token";
	const options={apiKey,env:{},transport:"sse"};
	if(input.replacement)options.onPayload=()=>input.replacement;
	const result=await stream(model,{messages},options).result();
	if(!input.fail&&result.errorMessage)throw Error(`${input.name}: ${result.errorMessage}`);
	if(typeof body!=="string")throw Error(`${input.name}: no request`);
	const fixture={name:input.name,model,messages,apiKey,payload,path,httpHeaders,body,stopReason:result.stopReason};
	if(result.errorMessage)fixture.errorMessage=result.errorMessage;
	if(input.replacement)fixture.replacement=input.replacement;
	if(input.fail)fixture.fail=input.fail;
	fixtures.push(fixture);
}
console.log(JSON.stringify(fixtures,null,2));
