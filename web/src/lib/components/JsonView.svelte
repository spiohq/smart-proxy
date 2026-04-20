<script lang="ts">
  let { data, maxHeight = '500px' }: { data: unknown; maxHeight?: string } = $props();

  interface Token {
    type: 'key' | 'string' | 'number' | 'boolean' | 'null' | 'brace' | 'comma' | 'colon' | 'ws';
    text: string;
  }

  function tokenize(json: string): Token[] {
    const tokens: Token[] = [];
    let i = 0;
    while (i < json.length) {
      const ch = json[i];
      if (ch === ' ' || ch === '\n') {
        let ws = '';
        while (i < json.length && (json[i] === ' ' || json[i] === '\n')) {
          ws += json[i];
          i++;
        }
        tokens.push({ type: 'ws', text: ws });
      } else if (ch === '{' || ch === '}' || ch === '[' || ch === ']') {
        tokens.push({ type: 'brace', text: ch });
        i++;
      } else if (ch === ',') {
        tokens.push({ type: 'comma', text: ',' });
        i++;
      } else if (ch === ':') {
        tokens.push({ type: 'colon', text: ': ' });
        i++;
        if (json[i] === ' ') i++;
      } else if (ch === '"') {
        let str = '"';
        i++;
        while (i < json.length && json[i] !== '"') {
          if (json[i] === '\\') {
            str += json[i];
            i++;
          }
          str += json[i];
          i++;
        }
        str += '"';
        i++;
        let j = i;
        while (j < json.length && json[j] === ' ') j++;
        const isKey = json[j] === ':';
        tokens.push({ type: isKey ? 'key' : 'string', text: str });
      } else if (json.slice(i, i + 4) === 'true') {
        tokens.push({ type: 'boolean', text: 'true' });
        i += 4;
      } else if (json.slice(i, i + 5) === 'false') {
        tokens.push({ type: 'boolean', text: 'false' });
        i += 5;
      } else if (json.slice(i, i + 4) === 'null') {
        tokens.push({ type: 'null', text: 'null' });
        i += 4;
      } else if (ch === '-' || (ch >= '0' && ch <= '9')) {
        let num = '';
        while (i < json.length && /[-0-9.eE+]/.test(json[i])) {
          num += json[i];
          i++;
        }
        tokens.push({ type: 'number', text: num });
      } else {
        i++;
      }
    }
    return tokens;
  }

  const colorMap: Record<string, string> = {
    key: 'text-secondary',
    string: 'text-secondary-fixed-dim',
    number: 'text-primary',
    boolean: 'text-tertiary',
    null: 'text-outline',
    brace: 'text-on-surface-variant',
    comma: 'text-outline-variant',
    colon: 'text-outline',
    ws: ''
  };

  let formatted = $derived(JSON.stringify(data, null, 2));
  let tokens = $derived(tokenize(formatted));
</script>

<pre
  class="rounded-xl bg-surface-container p-4 text-xs font-mono overflow-auto leading-relaxed border border-outline-variant/10"
  style="max-height: {maxHeight}"
>{#each tokens as tok}<span class={colorMap[tok.type]}>{tok.text}</span>{/each}</pre>
