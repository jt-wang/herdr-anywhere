Exported by Oh My Pi 19.0.0 against the local fakeresponses provider.

Regenerate with Bun and OMP on PATH:

    OMP_LIVE=/path/to/omp go test ./internal/chat/pi -run OMPLive -update

turns.jsonl contains real model replies, bash execution, an exit-code-3 failure,
and an image read. branch.jsonl is the same journal branched through OMP's own
SessionManager API after the first assistant message. Paths and timestamps are
normalized by the same scrubber used for Pi's fixtures.

externalized/images.jsonl is constructed sample data passed through OMP 19.0.0's
real prepareEntryForPersistence and BlobStore. Both a user image and a read tool
result reference the same deterministic 32x32 PNG (3172 bytes), stored under its
raw-byte SHA-256 in externalized/blobs. This exercises the >=1024-base64-char
externalization path without a model or real user data.

Tree regression cases in leaf_test.go follow the installed OMP 19.0.0 source:
session/session-manager.ts:3604-3630 preserves activeLeafId for off-branch appends;
session/session-entries.ts:434-435 selects that persisted leaf (including null).
session/session-manager.ts:4073-4081 only changes the in-memory leaf on branch/reset;
session/agent-session.ts:15520-15574 navigates without appending when there is no
summary and emits session_tree. The read-only extension context exposes getLeafId
and getEntries (session/session-manager.ts:774-803). Pi 1.4.0 has the same
non-persisting branch/reset behavior and tree event in dist/core/session-manager.js:
1161-1174 and dist/core/agent-session.js:3391-3414, but no activeLeafId persistence.

The v2 extensions label the selected leaf and journal tail together ("~" means
root/empty). The reader replays entries after that tail, so normal appends keep
the same selection identity; a tree switch without any write changes it. Old v1
templates remain recognized for safe update. These cases are synthetic sample
data, not captured private conversations.
