// Regenerate: node --experimental-strip-types ai/testdata/openrouter-images/generate.mjs ./pi > ai/testdata/openrouter-images/golden.json
import {registerHooks} from "node:module";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";
const root=resolve(process.argv[2]);
registerHooks({resolve(s,c,next){return s==="@earendil-works/pi-telemetry"?{url:pathToFileURL(resolve(root,"packages/telemetry/src/index.ts")).href,shortCircuit:true}:next(s,c);}});
const {generateImages}=await import(pathToFileURL(resolve(root,"packages/ai/src/api/openrouter-images.ts")));
const metadata={z:"1",a:"2"},custom={z:1,a:2};
const model={id:"google/gemini-2.5-flash-image",name:"Gemini Image",api:"openrouter-images",provider:"openrouter",baseUrl:"https://invalid.test/api/v1",input:["text","image"],output:["image","text"],cost:{input:0,output:0,cacheRead:0,cacheWrite:0}};
const context={input:[{type:"text",text:"a cat"},{type:"image",data:"QUJD",mimeType:"image/png"}]};
const cases=[
	{name:"default"},
	{name:"payload-replacement",replacement:{model:"replacement",messages:[{role:"user",content:"changed"}],stream:false,modalities:["image"],metadata,custom}},
	{name:"auth-error",fail:{status:401,body:'{"error":{"message":"no key"}}'}},
	{name:"text-error",fail:{status:403,body:"nope"}},
	{name:"empty-error",fail:{status:400,body:""}},
];
const fixtures=[];
for(const input of cases){
	let body,path;
	globalThis.fetch=async(url,init)=>{
		path=new URL(url).pathname;body=init.body;
		if(input.fail)return new Response(input.fail.body,{status:input.fail.status,headers:{"content-type":"application/json"}});
		return new Response(JSON.stringify({id:"r",choices:[{message:{content:"ok",images:[{image_url:"data:image/png;base64,QUJD"}]}}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}),{headers:{"content-type":"application/json"}});
	};
	let responded=false;
	const options={apiKey:"token",onPayload:input.replacement?()=>input.replacement:undefined,onResponse:()=>{responded=true;}};
	const result=await generateImages(model,context,options);
	if(!input.fail&&result.stopReason!=="stop")throw Error(`${input.name}: ${result.errorMessage??"no request"}`);
	if(typeof body!=="string")throw Error(`${input.name}: no request`);
	result.timestamp=1; // normalize so the golden regenerates byte-identically
	const fixture={name:input.name,model,input:context.input,path,body,responded,result:JSON.stringify(result)};
	if(input.fail)fixture.fail=input.fail;
	if(input.replacement)fixture.replacement=input.replacement;
	fixtures.push(fixture);
}
console.log(JSON.stringify(fixtures,null,2));
