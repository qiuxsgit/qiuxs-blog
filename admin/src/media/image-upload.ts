import type { AdminApi, MediaUploadPolicy, MediaView } from "../api/admin-api";

export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
export const MAX_FILENAME_BYTES = 255;
export const MAX_GFS_RESPONSE_BYTES = 64 * 1024;
export const GFS_UPLOAD_EXPIRE_SECONDS = "60";

const MIME_EXTENSIONS: Record<string, readonly string[]> = {
  "image/jpeg": ["jpg", "jpeg"],
  "image/png": ["png"],
  "image/webp": ["webp"],
  "image/gif": ["gif"],
};

export type UploadErrorCode =
  | "invalid_file"
  | "policy_failed"
  | "upload_failed"
  | "upload_canceled"
  | "malformed_response"
  | "registration_failed";

export class ImageUploadError extends Error {
  readonly code: UploadErrorCode;
  constructor(code: UploadErrorCode, message: string) {
    super(message);
    this.name = "ImageUploadError";
    this.code = code;
  }
}

export type FileValidation =
  | { ok: true; file: File; extension: string }
  | { ok: false; error: ImageUploadError };

const utf8Bytes = (value: string) => new TextEncoder().encode(value).byteLength;

export function validateMediaUploadPolicy(policy: MediaUploadPolicy, nowMs = Date.now()): ImageUploadError | undefined {
  try {
    const url = new URL(policy.uploadUrl);
    if (url.protocol !== "https:" || url.username || url.password || url.hash) throw new Error();
  } catch {
    return sanitizedFailure("policy_failed", "Unable to prepare image upload.");
  }
  if (policy.fileField !== "file" || !policy.appId || !policy.policy || !policy.signature || !policy.nonce) {
    return sanitizedFailure("policy_failed", "Unable to prepare image upload.");
  }
  const fields = [policy.appId, policy.policy, policy.signature, policy.timestamp, policy.expire, policy.nonce];
  if (fields.some((field) => field.length > 8192) || !/^\d{10}$/u.test(policy.timestamp) || policy.expire !== GFS_UPLOAD_EXPIRE_SECONDS) {
    return sanitizedFailure("policy_failed", "Unable to prepare image upload.");
  }
  const timestampMs = Number(policy.timestamp) * 1000;
  if (!Number.isSafeInteger(timestampMs) || timestampMs > nowMs + 300_000 || timestampMs + Number(policy.expire) * 1000 <= nowMs) {
    return sanitizedFailure("policy_failed", "Unable to prepare image upload.");
  }
  return undefined;
}

export function validateImageFile(file: File): FileValidation {
  const name = file.name;
  const basename = name.split(/[\\/]/u).pop() ?? "";
  const extension = basename.includes(".") ? basename.slice(basename.lastIndexOf(".") + 1).toLowerCase() : "";
  const extensions = MIME_EXTENSIONS[file.type];
  if (!extensions || !extensions.includes(extension)) {
    return { ok: false, error: new ImageUploadError("invalid_file", "Choose a supported image (JPEG, PNG, WebP, or GIF).") };
  }
  if (!name || name === "." || name === ".." || /[\\/\0]/u.test(name) || basename !== name) {
    return { ok: false, error: new ImageUploadError("invalid_file", "The image filename is invalid.") };
  }
  if (utf8Bytes(name) > MAX_FILENAME_BYTES) {
    return { ok: false, error: new ImageUploadError("invalid_file", "The image filename is too long.") };
  }
  if (file.size <= 0) return { ok: false, error: new ImageUploadError("invalid_file", "The image file is empty.") };
  if (file.size > MAX_IMAGE_BYTES) return { ok: false, error: new ImageUploadError("invalid_file", "Images must be 10 MiB or smaller.") };
  return { ok: true, file, extension };
}

export function buildUploadForm(policy: MediaUploadPolicy, file: File): FormData {
  const form = new FormData();
  form.append("appId", policy.appId);
  form.append("policy", policy.policy);
  form.append("signature", policy.signature);
  form.append("timestamp", policy.timestamp);
  form.append("expire", policy.expire);
  form.append("nonce", policy.nonce);
  form.append(policy.fileField, file, file.name);
  return form;
}

function sanitizedFailure(code: UploadErrorCode, message: string): ImageUploadError {
  return new ImageUploadError(code, message);
}

export async function parseGfsResponse(response: Response): Promise<number> {
  try {
    if (!response.ok) throw sanitizedFailure("upload_failed", "Image upload failed.");
    const length = response.headers.get("content-length");
    if (length && Number(length) > MAX_GFS_RESPONSE_BYTES) throw sanitizedFailure("malformed_response", "Image upload returned an invalid response.");
    const text = await response.text();
    if (new TextEncoder().encode(text).byteLength > MAX_GFS_RESPONSE_BYTES) throw sanitizedFailure("malformed_response", "Image upload returned an invalid response.");
    let value: unknown;
    try { value = JSON.parse(text); } catch { throw sanitizedFailure("malformed_response", "Image upload returned an invalid response."); }
    if (typeof value !== "object" || value === null || Array.isArray(value)) throw sanitizedFailure("malformed_response", "Image upload returned an invalid response.");
    const record = value as { code?: unknown; data?: unknown };
    const data = record.data as { val?: unknown } | undefined;
    if (record.code !== 0 || !data || typeof data.val !== "number" || !Number.isSafeInteger(data.val) || data.val <= 0) {
      throw sanitizedFailure("malformed_response", "Image upload returned an invalid response.");
    }
    return data.val;
  } finally {
    try { await response.body?.cancel(); } catch { /* response cleanup is best effort */ }
  }
}

export interface UploadTransportInput {
  policy: MediaUploadPolicy;
  form: FormData;
  signal: AbortSignal;
  onProgress(progress: number): void;
}

export type UploadTransport = (input: UploadTransportInput) => Promise<Response>;

function clampProgress(value: number): number {
  return Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0));
}

export function boundedProgress(value: number): number { return clampProgress(value); }

export function xhrTransport({ policy, form, signal, onProgress }: UploadTransportInput): Promise<Response> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    let settled = false;
    const finish = (callback: () => void) => { if (!settled) { settled = true; signal.removeEventListener("abort", abort); callback(); } };
    const abort = () => { xhr.abort(); finish(() => reject(new DOMException("aborted", "AbortError"))); };
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(boundedProgress((event.loaded / event.total) * 100));
    };
    xhr.onload = () => {
      const headers = new Headers();
      for (const line of xhr.getAllResponseHeaders().trim().split(/[\r\n]+/u)) {
        const separator = line.indexOf(":");
        if (separator > 0) headers.set(line.slice(0, separator).trim(), line.slice(separator + 1).trim());
      }
      onProgress(100);
      finish(() => resolve(new Response(xhr.responseText, { status: xhr.status, headers })));
    };
    xhr.onerror = () => finish(() => reject(new Error("upload failed")));
    xhr.onabort = () => finish(() => reject(new DOMException("aborted", "AbortError")));
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) return abort();
    try { xhr.open("POST", policy.uploadUrl); xhr.send(form); }
    catch (error) { finish(() => reject(error)); }
  });
}

export interface UploadImageInput {
  file: File;
  api: Pick<AdminApi, "createMediaUploadPolicy" | "registerMedia">;
  signal?: AbortSignal;
  onProgress?(progress: number): void;
  transport?: UploadTransport;
}

export async function uploadImage({ file, api, signal = new AbortController().signal, onProgress = () => undefined, transport = xhrTransport }: UploadImageInput): Promise<MediaView> {
  const validation = validateImageFile(file);
  if (!validation.ok) throw validation.error;
  if (signal.aborted) throw sanitizedFailure("upload_canceled", "Image upload canceled.");
  let policy: MediaUploadPolicy;
  try { policy = await api.createMediaUploadPolicy(signal); }
  catch (error) {
    if (signal.aborted || (error instanceof DOMException && error.name === "AbortError")) throw sanitizedFailure("upload_canceled", "Image upload canceled.");
    throw sanitizedFailure("policy_failed", "Unable to prepare image upload.");
  }
  const policyError = validateMediaUploadPolicy(policy);
  if (policyError) throw policyError;
  let gfsFileId: number;
  try {
    const response = await transport({ policy, form: buildUploadForm(policy, file), signal, onProgress: (value) => onProgress(clampProgress(value)) });
    gfsFileId = await parseGfsResponse(response);
  } catch (error) {
    if (signal.aborted || (error instanceof DOMException && error.name === "AbortError")) throw sanitizedFailure("upload_canceled", "Image upload canceled.");
    if (error instanceof ImageUploadError) throw error;
    throw sanitizedFailure("upload_failed", "Image upload failed.");
  }
  if (signal.aborted) throw sanitizedFailure("upload_canceled", "Image upload canceled.");
  try {
    const media = await api.registerMedia({ gfsFileId, originalName: file.name }, signal);
    if (!isValidMediaProxyUrl(media.url)) throw sanitizedFailure("registration_failed", "Unable to register image.");
    return media;
  }
  catch (error) {
    if (signal.aborted || (error instanceof DOMException && error.name === "AbortError")) throw sanitizedFailure("upload_canceled", "Image upload canceled.");
    throw sanitizedFailure("registration_failed", "Unable to register image.");
  }
}

export function isValidMediaProxyUrl(url: string): boolean {
  return /^\/img\/proxy\/[a-z0-9_-]+$/u.test(url);
}

export function escapeMarkdownDestination(url: string): string {
  return url.replace(/[\\<>]/gu, (character) => `\\${character}`);
}

export function insertMarkdownImage(markdown: string, url: string, offset = markdown.length, alt = "image"): string {
  const safeOffset = Math.max(0, Math.min(markdown.length, offset));
  const safeAlt = alt.replace(/[\\\[\]]/gu, (character) => `\\${character}`);
  const image = `![${safeAlt}](<${escapeMarkdownDestination(url)}>)`;
  const before = markdown.slice(0, safeOffset);
  const after = markdown.slice(safeOffset);
  const prefix = before.length > 0 && !/[\n]$/u.test(before) ? "\n" : "";
  const suffix = after.length > 0 && !/^[\n]/u.test(after) ? "\n" : "";
  return `${before}${prefix}${image}${suffix}${after}`;
}
