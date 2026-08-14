import type { BuilderConfigView, PutBuilderConfigRequest } from "../api/admin-api";
import { ApiProblem } from "../api/problem";

export type BuilderDraft = Omit<BuilderConfigView, "id" | "tokenConfigured"> & { token: string; tokenConfigured?: boolean };

export const defaults: BuilderDraft = {
  name: "",
  baseUrl: "",
  username: "",
  jobName: "",
  token: "",
  enabled: true,
};

const runeCount = (value: string) => Array.from(value).length;
const isAsciiDns = (hostname: string) => hostname.length > 0 && hostname.length <= 253 && hostname.split(".").every((label) => label.length > 0 && label.length <= 63 && !label.startsWith("-") && !label.endsWith("-") && /^[a-z0-9-]+$/u.test(label));

export function isCanonicalBuilderBaseUrl(raw: string): boolean {
  if (!raw || raw !== raw.trim() || !raw.startsWith("https://") || /[/?#]/u.test(raw.slice("https://".length))) return false;
  const authorityRaw = raw.slice("https://".length);
  if (/^\d+(?:\.\d+){3}(?::\d+)?$/u.test(authorityRaw)) {
    const hostRaw = authorityRaw.split(":", 1)[0] ?? "";
    if (hostRaw.split(".").some((part) => part.length > 1 && part.startsWith("0"))) return false;
  }
  let parsed: URL;
  try { parsed = new URL(raw); } catch { return false; }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.pathname !== "/" && parsed.pathname !== "" || parsed.search || parsed.hash) return false;
  const hostname = parsed.hostname;
  if (!hostname || hostname !== hostname.toLowerCase() || hostname.endsWith(".") || hostname.includes(":")) return false;
  const ipv4 = hostname.split(".");
  if (ipv4.every((part) => /^\d+$/u.test(part))) return ipv4.length === 4 && ipv4.every((part) => part.length <= 3 && Number(part) <= 255 && (part === "0" || !part.startsWith("0")));
  if (!isAsciiDns(hostname)) return false;
  const port = parsed.port;
  if (port && (port === "443" || Number(port).toString() !== port || Number(port) < 1 || Number(port) > 65535)) return false;
  const authority = hostname + (port ? `:${port}` : "");
  return parsed.host === authority && raw === `https://${authority}`;
}

export function isValidBuilderJobName(value: string): boolean {
  if (!value || new TextEncoder().encode(value).byteLength > 128 || !/^[A-Za-z0-9][A-Za-z0-9._/-]*$/u.test(value)) return false;
  return value.split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

export function validateBuilderDraft(draft: BuilderDraft, tokenConfigured = Boolean(draft.tokenConfigured)): string[] {
  const errors: string[] = [];
  if (!draft.name || draft.name !== draft.name.trim() || runeCount(draft.name) > 100) errors.push("name");
  if (!draft.username || draft.username !== draft.username.trim() || draft.username.includes(":" ) || runeCount(draft.username) > 255) errors.push("username");
  if (!isCanonicalBuilderBaseUrl(draft.baseUrl)) errors.push("baseUrl");
  if (!isValidBuilderJobName(draft.jobName)) errors.push("jobName");
  if (!tokenConfigured && !draft.token) errors.push("token");
  if (runeCount(draft.token) > 4096) errors.push("token");
  return errors;
}

export function buildBuilderPutRequest(draft: BuilderDraft): PutBuilderConfigRequest {
  const errors = validateBuilderDraft(draft);
  if (errors.length > 0) throw new Error(`Invalid builder settings: ${errors.join(", ")}`);
  const request: PutBuilderConfigRequest = { name: draft.name, baseUrl: draft.baseUrl, username: draft.username, jobName: draft.jobName, enabled: draft.enabled };
  if (draft.token !== "") request.token = draft.token;
  return request;
}

export function builderDraftFromView(view: BuilderConfigView): BuilderDraft {
  return { name: view.name, baseUrl: view.baseUrl, username: view.username, jobName: view.jobName, enabled: view.enabled, token: "", tokenConfigured: view.tokenConfigured };
}

export function clearBuilderToken(draft: BuilderDraft): BuilderDraft { return { ...draft, token: "" }; }
export function isBuilderTestEligible(draft: BuilderDraft, saved: boolean): boolean { return saved && draft.enabled && (Boolean(draft.tokenConfigured) || draft.token !== ""); }

export function builderProblemMessage(error: unknown): string {
  if (!(error instanceof ApiProblem)) return "Builder operation failed";
  if (error.status === 409) return "Builder settings changed on the server. Reload and try again.";
  if (error.status === 503 || error.status === 412) return "Jenkins is unavailable or rejected the connection.";
  return "Unable to save builder settings.";
}

export type BuilderLoadState = { kind: "empty" } | { kind: "configured" } | { kind: "error"; error: unknown };
export function builderLoadState(error: unknown): BuilderLoadState {
  if (error === undefined) return { kind: "configured" };
  if (error instanceof ApiProblem && error.status === 404) return { kind: "empty" };
  return { kind: "error", error };
}
