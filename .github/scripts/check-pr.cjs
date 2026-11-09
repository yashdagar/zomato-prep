const TYPES = ['content', 'feat', 'fix', 'ui', 'test', 'docs', 'chore'];
const TYPE_LABELS = TYPES.map(t => `type: ${t}`);
const AREA_PREFIX = 'area: ';
const SECTIONS = ['Summary', 'Verified', 'Not verified', 'Notes'];
const MIN_SUMMARY = 80;
const TITLE = new RegExp(`^(${TYPES.join('|')})(\\([a-z0-9-]+\\))?: \\S.{6,}$`);

function section(body, name) {
  const lines = body.replace(/<!--[\s\S]*?-->/g, '').split(/\r?\n/);
  const start = lines.findIndex(l => l.trim().toLowerCase() === `## ${name}`.toLowerCase());
  if (start < 0) return null;
  const rest = lines.slice(start + 1);
  const end = rest.findIndex(l => /^##\s/.test(l));
  return (end < 0 ? rest : rest.slice(0, end)).join('\n').trim();
}

function check({ title, body, labels }) {
  const problems = [];
  if (title.length > 72) problems.push(`Title is ${title.length} characters; keep it at 72 or fewer.`);
  if (!TITLE.test(title)) problems.push(`Title must look like "type(scope): summary" with type one of ${TYPES.join(', ')}. Scope is optional.`);
  for (const name of SECTIONS) {
    const text = section(body ?? '', name);
    if (text === null) problems.push(`Missing the "## ${name}" heading.`);
    else if (!text) problems.push(`"## ${name}" is empty.`);
    else if (name === 'Summary' && text.length < MIN_SUMMARY) problems.push(`"## Summary" has ${text.length} characters; write at least ${MIN_SUMMARY}.`);
  }
  const types = labels.filter(l => TYPE_LABELS.includes(l));
  const areas = labels.filter(l => l.startsWith(AREA_PREFIX));
  if (types.length !== 1) problems.push(`Add exactly one type label (${TYPE_LABELS.join(', ')}); found ${types.length}.`);
  if (areas.length < 1) problems.push(`Add at least one "${AREA_PREFIX}..." label.`);
  const prefix = (title.match(/^(\w+)/) || [])[1];
  if (types.length === 1 && TYPES.includes(prefix) && types[0] !== `type: ${prefix}`) problems.push(`Title type "${prefix}" does not match label "${types[0]}".`);
  return problems;
}

module.exports = { check };
