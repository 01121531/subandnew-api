# Mailbox card storage

CVV storage uses the database and the existing master encryption key, with
separate authenticated associated data. CVV has no automatic expiry.
Back up the database and master key together; backups now contain encrypted
CVV records. Losing the key makes the ciphertext unrecoverable.

Explicit `MAILBOX_TEMP_CVV_MODE=disabled|off|none` remains disabled.
Unset, `database`, and legacy `single_node|redis` use database storage.
Legacy memory/Redis CVV is not migrated. Reimport or provide missing values.
No old values lost during a restart can be recovered.

Reimport without CVV preserves the saved value unless the card number changes.
Changing a card clears its old CVV. Archive, reassignment, submission and
operator disablement revoke access, but retain the encrypted record.
Administrators can explicitly clear it with a version-checked operation.

Card conditions require mailbox view and credential permissions. Prefix and
suffix indexes use domain-separated HMAC digests, not plaintext card numbers.
The first filtered queries backfill up to 250 legacy cards per request.
Until all cards are indexed, queries fail explicitly and can be retried;
damaged ciphertext must be repaired before filtered results are available.
New imports and corrected cards update indexes in the same transaction.
Filter values are sent in POST bodies, never added to list URLs.

Use synthetic payment data for testing. Encryption and private networking
are not payment-data compliance certification. Do not use this feature
to retain real payment verification codes after authorization.
