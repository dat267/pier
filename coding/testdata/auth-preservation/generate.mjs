// Regenerate from the repository root (read-only upstream):
// node coding/testdata/auth-preservation/generate.mjs ./pi > coding/testdata/auth-preservation/golden.json
// pi v1.4.0 (4a080252), core/auth-storage.ts AuthStorage.modify/delete.
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
const root=resolve(process.argv[2] ?? './pi');
if (!execFileSync('git',['-C',root,'rev-parse','HEAD'],{encoding:'utf8'}).startsWith('4a080252')) throw Error('requires pi v1.4.0 (4a080252)');
const {AuthStorage,InMemoryAuthStorageBackend}=await import(pathToFileURL(resolve(root,'packages/coding-agent/src/core/auth-storage.ts')));
const cases=[
 {name:'future-type',initial:'{"known":{"type":"api_key","key":"synthetic-known","vendor":{"z":1,"a":"<keep>"}},"future":{"type":"future","opaque":"synthetic-future"}}'},
 {name:'mistyped-key',initial:'{"known":{"type":"api_key","key":"synthetic-known"},"mistyped":{"type":"api_key","key":5}}'},
 {name:'known-extra',initial:'{"known":{"type":"api_key","key":"synthetic-known","vendor":{"z":1,"a":"<keep>"}}}'},
 {name:'control',initial:'{"known":{"type":"api_key","key":"synthetic-known"}}'},
];
const results=[];
for(const c of cases) for(const op of ['modify','delete']) {
 const backend=new InMemoryAuthStorageBackend();
 backend.withLock(()=>({result:undefined,next:c.initial}));
 const store=AuthStorage.fromStorage(backend);
 if(op==='modify')await store.modify('added',async()=>({type:'api_key',key:'synthetic-added'}));
 else await store.delete('known');
 results.push({...c,op,expected:backend.withLock(current=>({result:current}))});
}
process.stdout.write(JSON.stringify(results,null,2)+'\n');
