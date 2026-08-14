import {describe,it,expect} from 'vitest';
import {loadBundle} from '../src/lib/bundle';
import {buildContentIndex,postUrl,tagUrl} from '../src/lib/content';
import {renderMarkdown} from '../src/lib/markdown';
describe('release bundle',()=>{it('loads fixture with filing and unique routes',async()=>{const b=await loadBundle();expect(b.schemaVersion).toBe(1);expect(b.site.filingNumber).toBeTruthy();expect(new Set(b.articles.map(a=>a.slug)).size).toBe(b.articles.length)});it('builds stable indexes',async()=>{const c=buildContentIndex(await loadBundle());expect(c.articles[0].slug).toBe('building-reliable-services');expect(postUrl('x')).toBe('/posts/x/');expect(tagUrl('go')).toBe('/tags/go/')})});
describe('markdown',()=>{it('renders gfm and metrics',async()=>{const r=await renderMarkdown('# Hello\n\n- [x] done\n\n```go\nconst x = 1\n```');expect(r.html).toContain('<h1>');expect(r.html).toContain('task-list');expect(r.wordCount).toBeGreaterThan(0);expect(r.headings[0].id).toBe('hello')});it('rejects raw html and unsafe urls',async()=>{await expect(renderMarkdown('<script>x</script>')).rejects.toThrow();await expect(renderMarkdown('[x](javascript:alert(1))')).rejects.toThrow()})});
