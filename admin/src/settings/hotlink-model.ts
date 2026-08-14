import type { HotlinkSettingsView, PutHotlinkSettingsRequest } from "../api/admin-api";
import type { components } from "../api/generated/admin";
import { ApiProblem } from "../api/problem";

export type HotlinkDraft = PutHotlinkSettingsRequest;
type HotlinkEntry = components["schemas"]["HotlinkEntry"];

export const defaults: HotlinkDraft = {
  allowEmptyReferer: true,
  entries: [
    { hostname: "qiuxs.com", enabled: true },
    { hostname: "blog-admin.qiuxs.com", enabled: true },
  ],
};

/** Mirrors service settings.NormalizeHostname. */
export function normalizeHostname(raw: string): string | undefined {
  let hostname = raw.trim();
  if (hostname.endsWith(".")) hostname = hostname.slice(0, -1);
  if (hostname === "" || hostname.length > 253 || /[^\x00-\x7f]/u.test(hostname)) return undefined;
  hostname = hostname.toLowerCase();
  if (/^[0-9.]+$/u.test(hostname)) return undefined;
  const labels = hostname.split(".");
  if (labels.some((label) => label.length === 0 || label.length > 63 || label.startsWith("-") || label.endsWith("-") || !/^[a-z0-9-]+$/u.test(label))) return undefined;
  return hostname;
}

function normalizedEntries(entries: readonly HotlinkEntry[]): { entries?: HotlinkEntry[]; duplicate: boolean } {
  const seen = new Set<string>();
  const result: HotlinkEntry[] = [];
  for (const entry of entries) {
    const hostname = normalizeHostname(entry.hostname);
    if (!hostname || seen.has(hostname)) return { duplicate: Boolean(hostname && seen.has(hostname)) };
    seen.add(hostname);
    result.push({ hostname, enabled: entry.enabled });
  }
  return { entries: result, duplicate: false };
}

export function validateHotlinkDraft(draft: HotlinkDraft): string[] {
  const normalized = normalizedEntries(draft.entries);
  return normalized.entries ? [] : ["entries"];
}

export function buildHotlinkPutRequest(draft: HotlinkDraft): PutHotlinkSettingsRequest {
  const normalized = normalizedEntries(draft.entries);
  if (!normalized.entries) throw new Error("Invalid hotlink settings: entries");
  return { allowEmptyReferer: draft.allowEmptyReferer, entries: normalized.entries };
}

export function draftFromHotlinkView(view: HotlinkSettingsView): HotlinkDraft {
  return { allowEmptyReferer: view.allowEmptyReferer, entries: view.entries.map((entry) => ({ ...entry })) };
}

export function applyHotlinkCache<T extends HotlinkSettingsView>(_previous: T, saved: T): T {
  return saved;
}

export function hotlinkConflictState(problem: unknown, draft: HotlinkDraft): { conflict: boolean; draft: HotlinkDraft; message: string | undefined } {
  if (problem instanceof ApiProblem && problem.status === 409 && problem.code === "settings_conflict") {
    return { conflict: true, draft, message: "Hotlink settings changed on the server. Your local changes are preserved." };
  }
  return { conflict: false, draft, message: undefined };
}
