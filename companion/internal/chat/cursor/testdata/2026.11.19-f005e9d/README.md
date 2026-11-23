# Cursor CLI 2026.11.19-f005e9d

Exported from the official CLI on 2026-11-22 with an isolated HOME and invented
paper-garden project. The approved account was used for six short prompts; no user
project or copied provider configuration was used.

store-before-resume.db contains a plain reply, approved shell, rejected shell,
exit-7 shell, image read and file edit. store.db adds a turn after quitting and
resuming the same conversation. garden.png is the exact invented image read.
SQLite backup exports include committed WAL contents. The schema is exported
from Cursor, not handwritten. Tests reopen copies in WAL mode as a live writer.

Before copying, an exact-key scan found no matches in the source fixtures.
The research CLI had persisted the key in a separate auth cache; that cache was
removed, never copied. These exports contain no authentication files.

Sanitization replaced home/host names and provider model identifiers, removed
opaque reasoning signatures and the session blob-encryption key. JSON blobs and
protobuf reference hashes were recalculated; protobuf field layout and schema
were preserved. No messages, tool results or errors were synthesized. The fixtures
were exported with a private sanitizer script. The cursor providerOptions key is
the format's namespace, not a copied provider/account name.
