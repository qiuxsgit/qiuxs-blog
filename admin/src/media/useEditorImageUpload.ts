import { useCallback, useRef, useState } from "react";

import { insertMarkdownImage, uploadImage, type UploadTransport } from "./image-upload";
import { useAuth } from "../auth/AuthProvider";

export interface EditorImageUploadOptions {
  onInsert(markdown: string): void;
  getMarkdown?(): string;
  getInsertionOffset?(): number;
  transport?: UploadTransport;
}

export function useEditorImageUpload({ onInsert, getMarkdown = () => "", getInsertionOffset = () => getMarkdown().length, transport }: EditorImageUploadOptions) {
  const { api } = useAuth();
  const controller = useRef<AbortController | undefined>(undefined);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string>();
  const [uploading, setUploading] = useState(false);

  const upload = useCallback(async (file: File) => {
    controller.current?.abort();
    const next = new AbortController();
    controller.current = next;
    setError(undefined);
    setProgress(0);
    setUploading(true);
    try {
      const uploadOptions = {
        api,
        file,
        signal: next.signal,
        onProgress: setProgress,
        ...(transport ? { transport } : {}),
      };
      const media = await uploadImage(uploadOptions);
      if (!next.signal.aborted) onInsert(insertMarkdownImage(getMarkdown(), media.url, getInsertionOffset(), media.originalName));
      return media;
    } catch (reason) {
      if (!next.signal.aborted) setError(reason instanceof Error ? reason.message : "Unable to upload image.");
      return undefined;
    } finally {
      if (controller.current === next) {
        controller.current = undefined;
        setUploading(false);
      }
    }
  }, [api, getInsertionOffset, getMarkdown, onInsert, transport]);

  const cancel = useCallback(() => controller.current?.abort(), []);
  return { upload, cancel, progress, uploading, error };
}
