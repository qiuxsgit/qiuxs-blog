# GFS Contract for Blog Media

The Blog Service integrates with a separately deployed Go File Server (GFS). This document pins the boundary used by the Service; changing any field, formula, endpoint, or redirect behavior requires coordinated compatibility review.

## Required GFS Revision

The deployed GFS commit must contain both of these commits:

- `f9b82569ffc5fca078053fd3fe048517fa61ab77` — exposes actual object metadata.
- `bcf87257e7425a6397c079aa9c9994eccbbf3aaa` — changes the final OSS read hop to a temporary redirect.

The GFS-backed OSS bucket must remain private. Neither the Blog Service nor the Admin stores a public OSS object URL.

## Upload Policy and Multipart Request

The Blog Service issues a policy valid for exactly 60 seconds. The decoded standard padded Base64 policy contains exactly:

```json
{"savePath":"blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}"}
```

The browser sends `POST {gfsBaseUrl}/v1/upload` as multipart form data with these fixed fields:

- `file`: the file part; this is the only file field.
- `appId`: configured GFS application ID.
- `policy`: standard padded Base64 policy.
- `signature`: lowercase hexadecimal upload signature.
- `timestamp`: decimal Unix timestamp in seconds.
- `expire`: the literal `60`.
- `nonce`: the issued 22-character nonce.

No caller may supply or override `savePath`.

GFS app registration returns the raw app secret once, while GFS stores its MD5 digest. Let `uploadSecret = lowerHex(MD5(rawAppSecret))`. The request signature is:

```text
lowerHex(MD5(appId + "_" + policy + "_" + timestamp + "_60_" + nonce + "_" + uploadSecret))
```

A successful upload is HTTP 200 with a GFS envelope whose `code` is `0`. The GFS object ID is `data.val`. `data.objectInfo` contains the actual object properties; its values, including `fileSize`, `format`, `imageWidth`, and `imageHeight`, are strings. The Service does not trust browser-supplied metadata and registers the object only after the metadata lookup below.

## Metadata Lookup

The Service requests:

```text
GET {gfsBaseUrl}/alioss/objects/{fileId}/metadata
```

The request has no GFS app secret, public-read secret, policy, or signature. Success requires HTTP 200, envelope `code: 0`, and `data.fileId` equal to the requested ID. The consumed response fields are:

```json
{
  "code": 0,
  "data": {
    "fileId": 41,
    "fileName": "photo.png",
    "fileSize": 8192,
    "contentType": "image/png",
    "imageMetadata": {
      "imageWidth": "640",
      "imageHeight": "480"
    }
  }
}
```

Both values inside `imageMetadata` are positive decimal strings. The Service uses a caller-supplied HTTP client with a five-second timeout, reads no more than 64 KiB, verifies the ID, and maps transport, status, envelope, body, and dimension failures to a sanitized dependency error.

## Locally Signed Reads

The Service never sends a read-signing request to GFS. It constructs a standard padded Base64 policy locally:

```json
{"userId":"","fileId":91,"imageWidth":0,"imageHeight":0,"internalFlag":0}
```

With the configured public-read secret, the read signature is:

```text
lowerHex(MD5(publicReadSecret + "_" + policy + "_" + timestamp + "_60_" + publicReadSecret))
```

The resulting short-lived URL is:

```text
GET {gfsBaseUrl}/read/{url-escaped-policy}?expire=60&signature={signature}&timestamp={timestamp}
```

GFS must respond to this read with a temporary `302` or `307` redirect to its signed OSS URL. It must not issue a permanent redirect, because the OSS target is short-lived and must not be cached as durable content.

## Secret Handling

The raw app secret, its MD5 digest, public-read secret, policies, nonces, signatures, signed URLs, metadata response bodies, and request URLs must not appear in errors or logs. Configuration errors name only the failing environment variable.
