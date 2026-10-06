-- Records how a user's approximate location was derived. The raw coordinate is
-- never stored; only a precision-5 geohash and this source marker are.
ALTER TABLE users ADD COLUMN approx_geohash_source TEXT;
