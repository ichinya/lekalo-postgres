#!/usr/bin/env node
import {dispatch, Refusal} from './provider.mjs';
const chunks = []; let size = 0;
for await (const chunk of process.stdin) {
  size += chunk.length;
  if (size > 8 * 1024 * 1024) throw new Error('request-size');
  chunks.push(chunk);
}
const request = JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(Buffer.concat(chunks)));
if (request.protocol !== 'lekalo/runtime/v1' || !/^sha256:[a-f0-9]{64}$/.test(request.request_id ?? '') || typeof request.operation !== 'string') throw new Error('request-envelope');
let result;
if (request.operation === 'describe') result = {id:'lekalo-postgres',version:'0.1.0',rules_api:['lekalo/contract-rules/v1'],capabilities:['storage.postgres'],operations:['check','storage.matrix','storage.type','storage.ddl','storage.table','storage.join','storage.default','storage.sequence','storage.index-name']};
else if (request.operation === 'check') {
  // SQL generation is explicit. This provider never claims source-code checks.
  if (request.input?.rules?.length) throw new Error('contract-checks-unsupported');
  result = {state:'complete',facts:{}};
} else {
  try { result = {value:dispatch(request.operation,request.input)}; }
  catch (error) {
    if (!(error instanceof Refusal)) throw error;
    result = {error:{kind:error.kind,detail:error.message}};
  }
}
process.stdout.write(JSON.stringify({protocol:request.protocol,request_id:request.request_id,result}));
