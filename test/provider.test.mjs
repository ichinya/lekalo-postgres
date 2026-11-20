import assert from 'node:assert/strict';
import {test} from 'node:test';
import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {dispatch,renderDdl,mapType,renderDefault,sequenceDefault,indexName,matrix} from '../src/provider.mjs';
const read = name => JSON.parse(readFileSync(new URL(name,import.meta.url)));
// This fixture is the external provider DTO, assembled from the existing host
// goldens. No runtime or host implementation is imported into the package.
function fixture() {
  const {projection}=read('engine-input.json'), attachment=read('attachment.json'), profile=read('profile.json');
  const declared=attachment.projections.find(p=>p.namespace==='postgres');
  const typed = (value,type) => ({kind:typeof value==='boolean'?'boolean':typeof value==='number'?'integer':type?.name==='decimal'?'decimal':'text',value:typeof value==='number'?String(value):value});
  for (const table of projection.tables) {
    const entity=attachment.entities.find(e=>e.entityKey===table.entity);
    const declaration=declared.tables.find(t=>t.entity===table.entity);
    table.enumMembers={};
    for (const column of table.columns) {
      column.generated=column.generatedKind;
      const field=entity.fields.find(f=>f.field===column.name);
      if (column.origin==='field' && field) column.sourceType=field.type;
      if (field?.type.name==='enum') table.enumMembers[column.name]=field.type.members;
      if (column.default?.kind==='literal') column.default.value=typed(column.default.value,field?.type);
    }
    table.checks=(declaration.checks ?? []).map(c=>({...c,predicates:c.where}));
    for (const check of table.checks) for (const p of check.predicates) if (p.value!==undefined) p.value=typed(p.value);
    for (const index of table.indexes ?? []) for (const p of index.where ?? []) if (p.value!==undefined) p.value=typed(p.value);
  }
  for (const join of projection.joins) {
    const relation=attachment.relations.find(r=>r.relationId===join.relation);
    join.references=[relation.owner,relation.target].map(e=>{const table=projection.tables.find(t=>t.entity===e);return {table:table.table,column:table.primaryKey[0]};});
  }
  return {profile,projection};
}
test('complete PostgreSQL DDL is byte-compatible with the pre-extraction golden',()=>{
  const {profile,projection}=fixture();
  const actual=renderDdl(profile,projection), expected=read('ddl.golden.json');
  assert.equal(actual.engineVersion,expected.engineVersion);
  assert.deepEqual(actual.statements,expected.statements);
});
test('version rows and capability differences are owner data',()=>{
  assert.deepEqual(matrix.rows.map(r=>r.major),[15,16,17,18]);
  for (const row of matrix.rows) {assert.equal(row.answers.length,17);assert.deepEqual(row.answers.map(a=>a[0]),matrix.rows[0].answers.map(a=>a[0]));}
  assert.equal(new Map(matrix.rows[0].answers).get('storage.postgres.sql_json'),'partial');
  assert.equal(new Map(matrix.rows[3].answers).get('storage.postgres.temporal_constraint'),'full');
});
test('type policies preserve native, JSON and refused cases',()=>{
  const p={json:'jsonb',array:'native',enum:'check'};
  for (const [name,sql] of Object.entries({boolean:'boolean',integer:'bigint',text:'text',uuid:'uuid',date:'date',timestamp:'timestamptz',binary:'bytea',json:'jsonb',enum:'varchar(64)'})) assert.equal(mapType({name},p),sql);
  assert.equal(mapType({name:'array',element:{name:'uuid'}},p),'uuid[]');
  assert.equal(mapType({name:'array',element:{name:'uuid'}},{...p,array:'json'}),'jsonb');
  assert.throws(()=>mapType({name:'array'},{...p,array:'unsupported'}),/array-unsupported/);
  assert.throws(()=>mapType({name:'enum'},{...p,enum:'native_enum'}),/native-enum/);
});
test('defaults preserve exact integers, decimals, quoting and bounded names',()=>{
  for (const value of ['-9223372036854775808','9223372036854775807']) assert.equal(renderDefault({kind:'literal',value:{kind:'integer',value}},'task'),value);
  assert.equal(renderDefault({kind:'literal',value:{kind:'decimal',value:'1234567890.123456789'}},'task'),'1234567890.123456789');
  assert.equal(renderDefault({kind:'literal',value:{kind:'text',value:"a'b"}},'task'),"'a''b'");
  assert.equal(sequenceDefault('task','id'),"nextval('seq_task_id')");
  assert.equal(indexName('task',{columns:['id'],unique:true}),'idx_task_id_uq');
  assert.throws(()=>sequenceDefault('a'.repeat(60),'id'),/sequence-name/);
  assert.throws(()=>dispatch('storage.execute',{}),/operation-unsupported/);
});
test('single-file binary echoes envelope and handles an external DTO',()=>{
  const request={protocol:'lekalo/runtime/v1',request_id:'sha256:'+'a'.repeat(64),operation:'storage.ddl',input:fixture()};
  const result=spawnSync(process.execPath,[fileURLToPath(new URL('../bin/lekalo-postgres.mjs', import.meta.url))],{input:JSON.stringify(request),encoding:'utf8'});
  assert.equal(result.status,0,result.stderr);
  const reply=JSON.parse(result.stdout); assert.equal(reply.request_id,request.request_id);assert.equal(reply.protocol,request.protocol);
  assert.deepEqual(reply.result.value.statements,read('ddl.golden.json').statements);
});

const goProvider = process.env.LEKALO_POSTGRES_GO_BIN;
test('Go executable matches the packaged provider over the runtime protocol', {skip: !goProvider}, () => {
  const {profile, projection} = fixture();
  const executable = fileURLToPath(new URL('../bin/lekalo-postgres.mjs', import.meta.url));
  const typePolicies = {json: 'jsonb', enum: 'check', array: 'native'};
  const cases = [
    ['describe', {}],
    ['check', {rules: []}],
    ['storage.matrix', {}],
    ['storage.ddl', {profile, projection}],
    ['storage.ddl', {profile: {...profile, engineVersion: '14.0.0'}, projection}],
    ['storage.table', {profile, table: projection.tables[0]}],
    ['storage.join', {join: projection.joins[0]}],
    ['storage.type', {policies: typePolicies, type: {name: 'array', element: {name: 'decimal', precision: 19, scale: 5}}}],
    ['storage.type', {policies: {...typePolicies, enum: 'native_enum'}, type: {name: 'enum'}}],
    ['storage.type', {policies: {...typePolicies, array: 'unsupported'}, type: {name: 'array'}}],
    ['storage.type', {policies: typePolicies, type: {name: 'tsvector'}}],
    ['storage.sequence', {table: 'task', column: 'id'}],
    ['storage.sequence', {table: 'a'.repeat(60), column: 'id'}],
    ['storage.index-name', {table: 'task', index: {columns: ['id'], unique: true}}],
    ['storage.execute', {}],
  ];
  for (const [kind, value] of [
    ['integer', '-9223372036854775808'],
    ['integer', '9223372036854775807'],
    ['decimal', '1234567890.123456789'],
    ['text', "a'é<&"],
    ['boolean', false],
    ['integer', 2147483647],
  ]) {
    cases.push(['storage.default', {table: 'task', value: {kind: 'literal', value: {kind, value}}}]);
  }
  cases.push(['storage.table', {
    profile,
    table: {
      ...projection.tables[0],
      columns: [{...projection.tables[0].columns[0], generated: 'computed'}],
    },
  }]);

  const requestID = 'sha256:' + 'a'.repeat(64);
  for (const [operation, input] of cases) {
    const request = JSON.stringify({protocol: 'lekalo/runtime/v1', request_id: requestID, operation, input});
    const options = {input: request, encoding: 'utf8', timeout: 5000};
    const legacy = spawnSync(process.execPath, [executable], options);
    const native = spawnSync(goProvider, [], options);
    assert.equal(legacy.status, 0, operation + ': ' + legacy.stderr);
    assert.equal(native.status, 0, operation + ': ' + native.stderr);
    assert.deepEqual(JSON.parse(native.stdout), JSON.parse(legacy.stdout), operation);
  }
});
