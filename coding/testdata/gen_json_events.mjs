// Run from the repository root: node --experimental-strip-types coding/testdata/gen_json_events.mjs
// Reference: pi v1.4.0 modes/json-event.ts. Output is label => JSON.stringify(event).
import { toJsonEvent } from '../../pi/packages/coding-agent/src/modes/json-event.ts';
const usage = {input:0,output:0,cacheRead:0,cacheWrite:0,totalTokens:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}};
const message = {role:'assistant',content:[],api:'openai-responses',provider:'openai',model:'mock',usage,stopReason:'stop',timestamp:1};
const call = {type:'toolCall',id:'c',name:'read',arguments:{z:1,a:2}};
const partial = {...message, content:[call]};
const emit = (label,event) => console.log(`${label} => ${JSON.stringify(toJsonEvent(event))}`);
for(const type of ['start','text_start','text_delta','text_end','thinking_start','thinking_delta','thinking_end','toolcall_start','toolcall_delta','toolcall_end','done','error']) {
 const inner = {type};
 if(type !== 'start' && type !== 'done' && type !== 'error') inner.contentIndex = 0;
 if(type.endsWith('_delta')) inner.delta = '';
 if(type === 'text_end' || type === 'thinking_end') inner.content = '';
 if(type === 'toolcall_end') inner.toolCall = call;
 if(type === 'done' || type === 'error') inner.reason = type === 'done' ? 'stop' : 'error';
 if(type === 'done') inner.message = message;
 else if(type === 'error') inner.error = message;
 else inner.partial = partial;
 emit(type, {type:'message_update', message, assistantMessageEvent:inner});
}
const result = {content:[{type:'text',text:'ok'},{type:'image',data:'AA==',mimeType:'image/png'}],details:{z:1,a:2},usage,isError:true,terminate:true};
const compaction = {summary:'summary',firstKeptEntryId:'e',tokensBefore:10,estimatedTokensAfter:2,usage,details:{z:1,a:2}};
const entry = {type:'custom',id:'e',parentId:null,timestamp:'2026-01-01T00:00:00Z',customType:'test',data:{z:1,a:2}};
for(const event of [
 {type:'agent_start'}, {type:'agent_end',messages:[],willRetry:false},
 {type:'agent_settled',aborted:false}, {type:'turn_start'},
 {type:'turn_end',message,toolResults:[]},
 {type:'message_start',message}, {type:'message_end',message},
 {type:'tool_execution_start',toolCallId:'c',toolName:'read',args:call.arguments},
 {type:'tool_execution_update',toolCallId:'c',toolName:'read',args:call.arguments,partialResult:result},
 {type:'tool_execution_end',toolCallId:'c',toolName:'read',result,isError:false,durationMs:0},
 {type:'queue_update',steering:[],followUp:[]},
 {type:'compaction_start',reason:'manual'},
 {type:'compaction_end',reason:'manual',result:compaction,aborted:false,willRetry:true},
 {type:'compaction_aborted',reason:'manual',result:undefined,aborted:true,willRetry:false,errorMessage:'cancelled'},
 {type:'entry_appended',entry}, {type:'session_info_changed'},
 {type:'thinking_level_changed',level:'high'},
 {type:'auto_retry_start',attempt:1,maxAttempts:3,delayMs:0,errorMessage:''},
 {type:'auto_retry_end',success:false,attempt:1,finalError:'failed'},
 {type:'summarization_retry_scheduled',attempt:1,maxAttempts:3,delayMs:0,errorMessage:''},
 {type:'summarization_retry_attempt_start',source:'compaction',reason:'manual'},
 {type:'summarization_retry_finished'},
 {type:'bash_execution_update',id:'b',delta:''},
]) {
 const label = event.type;
 if(label === 'compaction_aborted') event.type = 'compaction_end';
 emit(label, event);
}
