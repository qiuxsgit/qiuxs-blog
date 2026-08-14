import { z } from 'zod';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
const id = z.number().int().positive().max(9223372036854775807);
const checksum = z.string().regex(/^sha256:[0-9a-f]{64}$/);
export const BundleSchema = z.object({schemaVersion:z.literal(1),releaseId:id,generatedAt:z.string().datetime(),site:z.object({name:z.string().min(1),authorBio:z.string(),aboutMarkdown:z.string(),filingName:z.string().min(1),filingNumber:z.string().min(1),socialLinks:z.array(z.object({label:z.string(),url:z.string().url()}))}),tags:z.array(z.object({id,name:z.string(),slug:z.string().min(1)})),articles:z.array(z.object({articleId:id,revisionId:id,slug:z.string().min(1).regex(/^[a-z0-9-]+$/),title:z.string().min(1),summary:z.string(),contentMarkdown:z.string(),contentHash:checksum,publishedAt:z.string().datetime(),tags:z.array(z.string().min(1))})),checksum});
export type ReleaseBundle = z.infer<typeof BundleSchema>;
export async function loadBundle(path = process.env.BLOG_BUNDLE_PATH ?? resolve(process.cwd(),'../contracts/fixtures/release-bundle.v1.json')): Promise<ReleaseBundle> { const bundle=BundleSchema.parse(JSON.parse(await readFile(path,'utf8'))); if(new Set(bundle.articles.map(a=>a.slug)).size!==bundle.articles.length) throw new Error('duplicate article slug'); if(new Set(bundle.tags.map(t=>t.slug)).size!==bundle.tags.length) throw new Error('duplicate tag slug'); return bundle; }
