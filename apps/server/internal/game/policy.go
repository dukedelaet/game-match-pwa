package game

import (
	"encoding/json"

	"gamematch/internal/store"
)

// OrderedPair returns the two ids in ascending order (pairs are stored a < b).
func OrderedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// IntentsOK ports Safety::intentsOk: empty intents never match, and any
// interest in dating requires both sides to be dating.
func IntentsOK(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	aD := contains(a, "dating")
	bD := contains(b, "dating")
	if aD || bD {
		return aD && bD
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// AgeOK ports Safety::ageOk: mutual age-range fit, false when data is missing.
func AgeOK(aAge *int, aPref *store.Preference, bAge *int, bPref *store.Preference) bool {
	if aAge == nil || bAge == nil || aPref == nil || bPref == nil {
		return false
	}
	return *bAge >= aPref.AgeMin && *bAge <= aPref.AgeMax &&
		*aAge >= bPref.AgeMin && *aAge <= bPref.AgeMax
}

// GenderOK ports Safety::genderOk: an open preference accepts anyone.
func GenderOK(pref *store.Preference, otherGenderID *string) bool {
	if pref == nil || pref.WhoToMeetOpen {
		return true
	}
	if otherGenderID == nil {
		return false
	}
	var wanted []string
	if pref.WhoToMeet != nil {
		_ = json.Unmarshal([]byte(*pref.WhoToMeet), &wanted)
	}
	return contains(wanted, *otherGenderID)
}

// LevelForXP ports GameEngine::levelForXp: level n needs 100*n*(n-1)/2 XP.
func LevelForXP(xp int) int {
	level := 1
	for 100*level*(level+1)/2 <= xp {
		level++
		if level > 99 {
			break
		}
	}
	return level
}
