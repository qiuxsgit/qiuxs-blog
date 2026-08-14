import { z } from 'zod';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';
// Go's int64 values are numbers in the current bundle producer, while future
// producers may serialize them as decimal strings to avoid JSON precision loss.
// Accept both forms, but never accept an unsafe JavaScript number.
const id = z.union([z.number().int().positive().max(Number.MAX_SAFE_INTEGER), z.string().regex(/^[1-9][0-9]*$/)]);
const checksum = z.string().regex(/^sha256:[0-9a-f]{64}$/);
const safeUrl = z.string().url().refine((value) => /^https?:$/i.test(new URL(value).protocol), 'unsafe URL protocol');
export const BundleSchema = z.object({schemaVersion:z.literal(1),releaseId:id,generatedAt:z.string().datetime(),site:z.object({name:z.string().min(1),authorBio:z.string(),aboutMarkdown:z.string(),filingName:z.string().min(1),filingNumber:z.string().min(1),socialLinks:z.array(z.object({label:z.string(),url:safeUrl}).strict())}).strict(),tags:z.array(z.object({id,name:z.string(),slug:z.string().min(1)}).strict()),articles:z.array(z.object({articleId:id,revisionId:id,slug:z.string().min(1).regex(/^[a-z0-9-]+$/),title:z.string().min(1),summary:z.string(),contentMarkdown:z.string(),contentHash:checksum,publishedAt:z.string().datetime(),tags:z.array(z.string().min(1))}).strict()),checksum}).strict();
export type ReleaseBundle = z.infer<typeof BundleSchema>;
function checksumPayload(bundle: ReleaseBundle) { return JSON.stringify({site:bundle.site,tags:bundle.tags,articles:bundle.articles}); }
export function calculateChecksum(bundle: ReleaseBundle): string { return `sha256:${createHash('sha256').update(checksumPayload(bundle)).digest('hex')}`; }
export async function loadBundle(path = process.env.BLOG_BUNDLE_PATH ?? resolve(process.cwd(),'../contracts/fixtures/release-bundle.v1.json')): Promise<ReleaseBundle> { const bundle=BundleSchema.parse(JSON.parse(await readFile(path,'utf8'))); if(new Set(bundle.articles.map(a=>a.slug)).size!==bundle.articles.length) throw new Error('duplicate article slug'); if(new Set(bundle.tags.map(t=>t.slug)).size!==bundle.tags.length) throw new Error('duplicate tag slug'); if(calculateChecksum(bundle)!==bundle.checksum) throw new Error('release bundle checksum mismatch'); return bundle; }
