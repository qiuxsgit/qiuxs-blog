import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkRehype from 'remark-rehype';
import rehypeStringify from 'rehype-stringify';
export type Heading={depth:number;id:string;text:string};
const slug=(s:string)=>s.toLowerCase().trim().replace(/[^\p{L}\p{N}\s-]/gu,'').replace(/[\s]+/g,'-');
export async function renderMarkdown(markdown:string){
  if(/<\/?[a-z][^>]*>/i.test(markdown)) throw new Error('raw HTML is not allowed');
  if(/(?:javascript|data):/i.test(markdown)) throw new Error('unsafe URL');
  const headings:Heading[]=[]; const used=new Map<string,number>();
  const tree=await unified().use(remarkParse).use(remarkGfm).run(unified().use(remarkParse).use(remarkGfm).parse(markdown));
  for(const n of (tree as any).children){ if(n.type==='heading'){const text=n.children?.filter((x:any)=>x.type==='text').map((x:any)=>x.value).join('')??''; const base=slug(text)||'section'; const count=used.get(base)??0; used.set(base,count+1); headings.push({depth:n.depth,id:count?`${base}-${count}`:base,text}); }}
  const file=await unified().use(remarkParse).use(remarkGfm).use(remarkRehype,{allowDangerousHtml:false}).use(rehypeStringify).process(markdown);
  const html=String(file);
  const words=(markdown.match(/[\p{L}\p{N}]+/gu)??[]).length;
  return {html,headings,wordCount:words,readingMinutes:Math.max(1,Math.ceil(words/300))};
}
