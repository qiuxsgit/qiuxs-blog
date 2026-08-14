import { ApiProblem } from "./problem";

export type EntityId = number;

export function requireEntityId(value: unknown, field: string): EntityId {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) {
    throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
  }
  return value;
}

const entityIdFields = new Set([
  "id",
  "articleId",
  "draftRevisionId",
  "publishedRevisionId",
  "coverMediaId",
  "tagId",
  "mediaId",
  "gfsFileId",
  "seoDefaultImageMediaId",
  "releaseId",
  "builderId",
]);

export function requireResponseEntityIds(value: unknown, field = "response"): void {
  if (Array.isArray(value)) {
    value.forEach((item, index) => requireResponseEntityIds(item, `${field}[${index}]`));
    return;
  }
  if (typeof value !== "object" || value === null) {
    return;
  }
  for (const [key, child] of Object.entries(value)) {
    const childField = `${field}.${key}`;
    if (entityIdFields.has(key)) {
      if (child !== null) {
        requireEntityId(child, childField);
      }
      continue;
    }
    requireResponseEntityIds(child, childField);
  }
}
