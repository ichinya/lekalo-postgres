// PostgreSQL policy and deterministic DDL implementation. Inputs contain only
// declared data; this module never connects to a database or evaluates SQL.
import matrix from './matrix.json' with {type:'json'};
export {matrix};
export class Refusal extends Error {
  constructor(detail, kind = 'mapping') { super(detail); this.kind = kind; }
}
const refuse = (detail, kind) => { throw new Refusal(detail, kind); };
export function name(value, detail = 'identifier') {
  if (typeof value !== 'string' || !/^[a-z][a-z0-9_]{0,62}$/.test(value)) refuse(detail);
  return value;
}
export const quote = value => `"${name(value)}"`;
const literal = value => {
  switch (value?.kind) {
    case 'boolean': if (typeof value.value !== 'boolean') refuse('literal'); return value.value ? 'TRUE' : 'FALSE';
    // Integers cross the wire as decimal text, preserving the full i64 range.
    case 'integer': if (!/^-?\d+$/.test(value.value)) refuse('literal'); return value.value;
    case 'decimal': if (!/^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(value.value)) refuse('literal'); return value.value;
    case 'text': if (typeof value.value !== 'string') refuse('literal'); return `'${value.value.replaceAll("'", "''")}'`;
    default: return refuse('literal');
  }
};
export function mapType(type, policies, depth = 0) {
  if (depth > 32) refuse('type-depth');
  switch (type?.name) {
    case 'boolean': return 'boolean';
    case 'integer': return 'bigint';
    case 'decimal': if (!Number.isInteger(type.precision) || !Number.isInteger(type.scale)) refuse('decimal-type'); return `numeric(${type.precision},${type.scale})`;
    case 'string': if (!Number.isInteger(type.length) || type.length <= 0) refuse('string-type'); return `varchar(${type.length})`;
    case 'text': return 'text';
    case 'uuid': return 'uuid';
    case 'date': return 'date';
    case 'timestamp': return 'timestamptz';
    case 'binary': return 'bytea';
    case 'json': if (!['json','jsonb'].includes(policies.json)) refuse('json-policy'); return policies.json;
    case 'enum': if (policies.enum === 'native_enum') refuse('native-enum','render'); if (policies.enum !== 'check') refuse('enum-policy'); return 'varchar(64)';
    case 'array':
      if (policies.array === 'unsupported') refuse('array-unsupported','render');
      if (policies.array === 'json') return 'jsonb';
      if (policies.array !== 'native') refuse('array-policy');
      return `${mapType(type.element, policies, depth + 1)}[]`;
    default: return refuse('domain-type');
  }
}
export const sequenceDefault = (table, column) => `nextval('${name(`seq_${name(table)}_${name(column)}`, 'sequence-name')}')`;
export function renderDefault(value, table) {
  switch (value?.kind) {
    case 'literal': return literal(value.value);
    case 'now': return 'now()';
    case 'uuid_generate': return 'gen_random_uuid()';
    case 'sequence': return sequenceDefault(table, value.ref);
    default: return refuse('default-kind');
  }
}
export const indexName = (table, index) => name(`idx_${name(table)}_${index.columns.map(c=>name(c)).join('_')}${index.unique ? '_uq' : ''}`, 'index-name');
function predicates(rows) {
  return rows.map(row => {
    const column = quote(row.column);
    if (row.op === 'is-null') return `${column} IS NULL`;
    if (row.op === 'is-not-null') return `${column} IS NOT NULL`;
    if (!['eq','ne'].includes(row.op)) refuse('predicate');
    if (row.value === null || row.value === undefined) return `${column} IS NOT NULL`;
    return `${column} ${row.op === 'eq' ? '=' : '<>'} ${literal(row.value)}`;
  }).join(' AND ');
}
export function columnType(profile, column) {
  if (column.sourceType) return mapType(column.sourceType, profile.policies);
  if (typeof column.type !== 'string' || column.type.length > 512 || /[;\n\r\0]/.test(column.type)) refuse('storage-type');
  return column.type;
}
export function createTable(profile, table) {
  const enums = table.enumMembers ?? {};
  if (Object.keys(enums).length && profile.policies.enum === 'native_enum') refuse('native-enum','render');
  const lines = table.columns.map(column => {
    let line = `${quote(column.name)} ${columnType(profile,column)}`;
    if (column.generated === 'identity') line += ' GENERATED ALWAYS AS IDENTITY';
    else if (column.generated === 'computed') refuse('computed-column','render');
    else if (column.generated === 'sequence') line += ` DEFAULT ${sequenceDefault(table.table,column.name)}`;
    else if (column.default) line += ` DEFAULT ${renderDefault(column.default,table.table)}`;
    if (!column.nullable) line += ' NOT NULL';
    return line;
  });
  lines.push(`CONSTRAINT "pk_${name(table.table)}" PRIMARY KEY (${table.primaryKey.map(c=>quote(c)).join(', ')})`);
  for (const check of table.checks ?? []) lines.push(`CONSTRAINT ${quote(check.name)} CHECK (${predicates(check.predicates)})`);
  for (const column of table.columns) {
    const members = enums[column.name];
    if (!members) continue;
    const constraint = name(`chk_${table.table}_${column.name}`, 'check-name');
    lines.push(`CONSTRAINT ${quote(constraint)} CHECK (${quote(column.name)} IN (${members.map(v=>literal({kind:'text',value:v})).join(', ')}))`);
  }
  return `CREATE TABLE ${quote(table.table)} (${lines.join(', ')});`;
}
export function createJoin(join) {
  const lines = join.columns.map(c=>`${quote(c.name)} ${columnType({policies:{}},c)} NOT NULL`);
  if (join.uniquePair) lines.push(`CONSTRAINT "pk_${name(join.table)}" PRIMARY KEY (${join.columns.map(c=>quote(c.name)).join(', ')})`);
  return `CREATE TABLE ${quote(join.table)} (${lines.join(', ')});`;
}
export function renderDdl(profile, projection) {
  if (!matrix.rows.some(row => row.major === Number(profile.engineVersion?.split('.')[0]))) refuse('major-unpublished');
  const statements = [], owners = [];
  for (const extension of profile.extensions ?? []) if (extension.required || extension.state === 'required') statements.push(`CREATE EXTENSION IF NOT EXISTS ${quote(extension.name)};`);
  for (const table of projection.tables) for (const column of table.columns) {
    if (column.generated !== 'sequence') continue;
    const sequence = name(`seq_${table.table}_${column.name}`, 'sequence-name');
    statements.push(`CREATE SEQUENCE ${quote(sequence)};`);
    owners.push([sequence,`${quote(table.table)}.${quote(column.name)}`]);
  }
  for (const table of projection.tables) statements.push(createTable(profile,table));
  for (const join of projection.joins) statements.push(createJoin(join));
  const keys = new Map(projection.tables.map(t=>[t.table,t.primaryKey[0]]));
  const foreignKey = (table,column,target,key,action) => {
    if (!key) refuse('referenced-key-absent');
    if (!['CASCADE','RESTRICT','SET NULL','NO ACTION','SET DEFAULT'].includes(action)) refuse('delete-action');
    return `ALTER TABLE ${quote(table)} ADD CONSTRAINT ${quote(name(`fk_${table}_${column}`, 'foreign-key-name'))} FOREIGN KEY (${quote(column)}) REFERENCES ${quote(target)}(${quote(key)}) ON DELETE ${action};`;
  };
  for (const table of projection.tables) for (const fk of table.foreignKeys ?? []) statements.push(foreignKey(table.table,fk.column,fk.referencesTable,keys.get(fk.referencesTable),fk.onDelete));
  for (const join of projection.joins) for (const [i,action] of [join.onOwnerDelete,join.onTargetDelete].entries()) {
    const ref = join.references?.[i]; if (!ref) refuse('join-reference-unresolved');
    statements.push(foreignKey(join.table,join.columns[i].name,ref.table,ref.column,action));
  }
  for (const table of projection.tables) for (const index of table.indexes ?? []) {
    const where = index.where ? ` WHERE ${predicates(index.where)}` : '';
    statements.push(`CREATE ${index.unique ? 'UNIQUE ' : ''}INDEX ${quote(index.name ?? indexName(table.table,index))} ON ${quote(table.table)} (${index.columns.map(c=>quote(c)).join(', ')})${where};`);
  }
  for (const [sequence,owner] of owners) statements.push(`ALTER SEQUENCE ${quote(sequence)} OWNED BY ${owner};`);
  if (profile.tenancy?.enforcement === 'rls') {
    const rls = profile.tenancy.rls; if (!rls) refuse('rls-policy-missing');
    if (typeof rls.sessionVariable !== 'string' || rls.sessionVariable.length < 3 || rls.sessionVariable.length > 63 || !/^[a-z][a-z0-9_]*(\.[a-z0-9_]+)*$/.test(rls.sessionVariable)) refuse('rls-session-variable');
    for (const table of projection.tables) {
      const tenant = table.columns.find(c=>c.origin === 'tenant_key'); if (!tenant) continue;
      statements.push(`ALTER TABLE ${quote(table.table)} ENABLE ROW LEVEL SECURITY;`);
      if (rls.force) statements.push(`ALTER TABLE ${quote(table.table)} FORCE ROW LEVEL SECURITY;`);
      statements.push(`CREATE POLICY ${quote(name(`pol_${table.table}_tenant`, 'policy-name'))} ON ${quote(table.table)} USING (${quote(tenant.name)} = current_setting('${rls.sessionVariable}')::${tenant.type});`);
    }
  }
  return {engineVersion:profile.engineVersion,statements:statements.map((statement,i)=>({id:i+1,statement}))};
}
export function dispatch(operation, input) {
  switch (operation) {
    case 'storage.matrix': return matrix;
    case 'storage.type': return mapType(input.type,input.policies);
    case 'storage.ddl': return renderDdl(input.profile,input.projection);
    case 'storage.table': return createTable(input.profile,input.table);
    case 'storage.join': return createJoin(input.join);
    case 'storage.default': return renderDefault(input.value,input.table);
    case 'storage.sequence': return sequenceDefault(input.table,input.column);
    case 'storage.index-name': return indexName(input.table,input.index);
    default: return refuse('operation-unsupported');
  }
}
