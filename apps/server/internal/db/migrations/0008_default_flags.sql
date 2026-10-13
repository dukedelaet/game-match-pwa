-- Sign-in is gated on two feature flags: auth.public_signup lets unknown
-- phone numbers request a code, and auth.otp actually sends the SMS. FlagOn
-- returns false for a missing row, so without these defaults a freshly
-- migrated production database silently accepts no one. seed writes
-- auth.public_signup but nothing has ever written auth.otp, which is what
-- made the bootstrap circular.
INSERT OR IGNORE INTO feature_flags ("key", value) VALUES ('auth.public_signup', '{"v":true}');
INSERT OR IGNORE INTO feature_flags ("key", value) VALUES ('auth.otp', '{"v":true}');