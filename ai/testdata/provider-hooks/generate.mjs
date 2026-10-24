// Regenerate: node --experimental-strip-types ai/testdata/provider-hooks/generate.mjs ./pi > ai/testdata/provider-hooks/golden.json
import {zstdDecompressSync} from "node:zlib";
import {registerHooks} from "node:module";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";
const root=resolve(process.argv[2]);
registerHooks({resolve(s,c,next){return s==="@earendil-works/pi-telemetry"?{url:pathToFileURL(resolve(root,"packages/telemetry/src/index.ts")).href,shortCircuit:true}:next(s,c);}});
const metadata={z:"1",a:"2"},custom={z:1,a:2};
const cases=[
	{api:"openai-completions",id:"gpt-4o",provider:"openai",baseUrl:"https://invalid.test/v1",replacement:{model:"replacement",messages:[{role:"user",content:"changed"}],stream:true,metadata,custom}},
	{api:"anthropic-messages",id:"claude-sonnet-4-5",provider:"anthropic",baseUrl:"https://invalid.test",replacement:{model:"replacement",messages:[{role:"user",content:"changed"}],max_tokens:8,stream:false,metadata,custom}},
	{api:"openai-codex-responses",id:"gpt-5-codex",provider:"openai-codex",baseUrl:"https://invalid.test/v1",replacement:{model:"replacement",input:[{role:"user",content:"changed"}],stream:true,store:false,metadata,custom}},
];
const fixtures=[];
for(const input of cases){
	const {stream}=await import(pathToFileURL(resolve(root,`packages/ai/src/api/${input.api}.ts`)));
	let body,path,payload;
	globalThis.fetch=async(url,init)=>{
		path=new URL(url).pathname;
		body=new Headers(init.headers).get("content-encoding")==="zstd"?zstdDecompressSync(init.body).toString("utf8"):init.body;
		let event='data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}\n\n';
		if(input.api==="openai-completions")event='data: {"id":"r","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n';
		if(input.api==="anthropic-messages")event='event: message_start\ndata: {"type":"message_start","message":{"id":"r","model":"claude-sonnet-4-5","usage":{"input_tokens":1,"output_tokens":0}}}\n\nevent: message_delta\ndata: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}\n\nevent: message_stop\ndata: {"type":"message_stop"}\n\n';
		return new Response(event,{headers:{"content-type":"text/event-stream"}});
	};
	const model={api:input.api,id:input.id,provider:input.provider,baseUrl:input.baseUrl,reasoning:false,maxTokens:8192,contextWindow:128000,input:["text","image"],cost:{input:0,output:0,cacheRead:0,cacheWrite:0}};
	const messages=[{role:"user",content:"hello",timestamp:1}];
	const apiKey=input.api==="openai-codex-responses"?`h.${Buffer.from(JSON.stringify({"https://api.openai.com/auth":{chatgpt_account_id:"acct"}})).toString("base64url")}.s`:"token";
	const result=await stream(model,{messages},{apiKey,env:{},transport:"sse",onPayload(p){payload=JSON.stringify(p);return input.replacement;}}).result();
	if(result.errorMessage||typeof body!=="string")throw Error(result.errorMessage??"no request");
	fixtures.push({model,messages,replacement:input.replacement,payload,path,body});
}
console.log(JSON.stringify(fixtures,null,2));
