import { describe, expect, it, vi } from "vitest";

import type { MediaUploadPolicy, MediaView } from "../api/admin-api";
import {
  MAX_FILENAME_BYTES,
  MAX_GFS_RESPONSE_BYTES,
  MAX_IMAGE_BYTES,
  ImageUploadError,
  buildUploadForm,
  boundedProgress,
  insertMarkdownImage,
  parseGfsResponse,
  uploadImage,
  validateImageFile,
} from "./image-upload";

const policy: MediaUploadPolicy = {
  uploadUrl: "https://gfs.invalid/upload",
  appId: "app",
  policy: "secret-policy",
  signature: "secret-signature",
  timestamp: "1",
  expire: "2",
  nonce: "n",
  fileField: "file",
};

const media = { id: 1, publicKey: "key", gfsFileId: 7, originalName: "a.png", mimeType: "image/png", fileSize: 1, width: 1, height: 1, state: "active", url: "https://blog.invalid/img/proxy/1", createdAt: "2025-01-01T00:00:00Z", updatedAt: "2025-01-01T00:00:00Z" } as MediaView;
const file = (name = "a.png", type = "image/png", size = 1) => new File([new Uint8Array(size)], name, { type });
const response = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

describe("direct image upload protocol", () => {
  it("accepts only matching supported extensions, names, and size boundaries", () => {
    expect(validateImageFile(file()).ok).toBe(true);
    expect(validateImageFile(file("a.jpg", "image/png")).ok).toBe(false);
    expect(validateImageFile(file("../a.png")).ok).toBe(false);
    expect(validateImageFile(file("a.png", "image/png", 0)).ok).toBe(false);
    expect(validateImageFile(file("a.png", "image/png", MAX_IMAGE_BYTES + 1)).ok).toBe(false);
    expect(validateImageFile(file(`${"界".repeat(130)}.png`)).ok).toBe(false);
    expect(new TextEncoder().encode("a.png").byteLength).toBeLessThan(MAX_FILENAME_BYTES);
  });

  it("builds the exact bodyless-policy multipart fields", () => {
    const form = buildUploadForm(policy, file());
    expect([...form.keys()]).toEqual(["appId", "policy", "signature", "timestamp", "expire", "nonce", "file"]);
    expect(form.get("appId")).toBe("app");
    expect(form.get("file")).toBeInstanceOf(File);
  });

  it("accepts only code zero and a positive integer data.val", async () => {
    expect(await parseGfsResponse(response({ code: 0, data: { val: 7 } }))).toBe(7);
    for (const body of [{ code: 1, data: { val: 7 } }, { code: 0, data: { val: 0 } }, { code: 0, data: { val: 1.2 } }, { code: 0, data: { val: "7" } }, { code: 0 }]) {
      await expect(parseGfsResponse(response(body))).rejects.toMatchObject({ code: "malformed_response" });
    }
  });

  it("rejects oversized responses and always attempts body cleanup", async () => {
    const cancel = vi.fn(async () => undefined);
    const body = "x".repeat(MAX_GFS_RESPONSE_BYTES + 1);
    const oversized = new Response(body, { status: 200, headers: { "content-type": "application/json" } });
    Object.defineProperty(oversized, "body", { value: { cancel } });
    await expect(parseGfsResponse(oversized)).rejects.toMatchObject({ code: "malformed_response" });
    expect(cancel).toHaveBeenCalled();
  });

  it("registers only the GFS ID and original name, never policy secrets", async () => {
    const createPolicy = vi.fn(async () => policy);
    const register = vi.fn(async () => media);
    const transport = vi.fn(async ({ onProgress }: { onProgress: (value: number) => void }) => {
      onProgress(-1); onProgress(50); onProgress(101);
      return response({ code: 0, data: { val: 7 } });
    });
    await uploadImage({ file: file(), api: { createMediaUploadPolicy: createPolicy, registerMedia: register }, transport });
    expect(createPolicy).toHaveBeenCalledTimes(1);
    expect(register).toHaveBeenCalledWith({ gfsFileId: 7, originalName: "a.png" }, expect.any(AbortSignal));
    expect(JSON.stringify(register.mock.calls)).not.toContain("secret-signature");
  });

  it("cancels transport and skips registration", async () => {
    const controller = new AbortController();
    const register = vi.fn();
    const transport = vi.fn(async ({ signal }: { signal: AbortSignal }) => {
      controller.abort();
      expect(signal.aborted).toBe(true);
      throw new DOMException("aborted", "AbortError");
    });
    await expect(uploadImage({ file: file(), api: { createMediaUploadPolicy: async () => policy, registerMedia: register }, signal: controller.signal, transport })).rejects.toMatchObject({ code: "upload_canceled" });
    expect(register).not.toHaveBeenCalled();
  });

  it("bounds progress and inserts only the returned URL", () => {
    expect(boundedProgress(-1)).toBe(0);
    expect(boundedProgress(101)).toBe(100);
    expect(insertMarkdownImage("# title", "https://example.test/a b.png?x=<y>")).toBe("# title\n![image](<https://example.test/a b.png?x=\\<y\\>>)");
  });

  it("does not expose secret-bearing transport failures", async () => {
    const transport = vi.fn(async () => { throw new Error(`signature=${policy.signature}`); });
    await expect(uploadImage({ file: file(), api: { createMediaUploadPolicy: async () => policy, registerMedia: vi.fn() }, transport })).rejects.toSatisfy((error: unknown) => error instanceof ImageUploadError && !error.message.includes(policy.signature));
  });
});
