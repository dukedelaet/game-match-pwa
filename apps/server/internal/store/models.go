package store

// Row models. Nullable columns are pointers; booleans map to SQLite INTEGER.

type User struct {
	ID             string  `db:"id"`
	Name           *string `db:"name"`
	Email          *string `db:"email"`
	PhoneE164Hash  *string `db:"phone_e164_hash"`
	Role           string  `db:"role"`
	OnboardingStep string  `db:"onboarding_step"`
	Status         string  `db:"status"`
	AgeAttestedAt  *string `db:"age_attested_at"`
	Dob            *string `db:"dob"`
	Incognito      bool    `db:"incognito"`
	Hidden         bool    `db:"hidden"`
	LastSeenAt     *string `db:"last_seen_at"`
	LastActiveOn   *string `db:"last_active_on"`
	MetroID        *string `db:"metro_id"`
	ApproxGeohash  *string `db:"approx_geohash"`
	GeohashSource  *string `db:"approx_geohash_source"`
	XP             int     `db:"xp"`
	Level          int     `db:"level"`
	CreatedAt      *string `db:"created_at"`
	UpdatedAt      *string `db:"updated_at"`
}

type Metro struct {
	ID          string   `db:"id"`
	Slug        string   `db:"slug"`
	Label       string   `db:"label"`
	CentroidLat *float64 `db:"centroid_lat"`
	CentroidLng *float64 `db:"centroid_lng"`
	AdjacentIDs *string  `db:"adjacent_ids"`
	CreatedAt   *string  `db:"created_at"`
	UpdatedAt   *string  `db:"updated_at"`
}

type Gender struct {
	ID     string `db:"id"`
	Slug   string `db:"slug"`
	Label  string `db:"label"`
	Sort   int    `db:"sort"`
	Active bool   `db:"active"`
}

type Trait struct {
	ID    string  `db:"id"`
	Slug  string  `db:"slug"`
	Label string  `db:"label"`
	Emoji *string `db:"emoji"`
	Sort  int     `db:"sort"`
	Axis  string  `db:"axis"`
}

// BehaviorStats holds the counters behind the scorer's behavior vector.
type BehaviorStats struct {
	UserID       string `db:"user_id"`
	RiskN        int    `db:"risk_n"`
	RiskSum      int    `db:"risk_sum"`
	TempoN       int    `db:"tempo_n"`
	TempoFast    int    `db:"tempo_fast"`
	RematchN     int    `db:"rematch_n"`
	RematchYes   int    `db:"rematch_yes"`
	GuessN       int    `db:"guess_n"`
	GuessCorrect int    `db:"guess_correct"`
}

type Profile struct {
	UserID        string  `db:"user_id"`
	Age           *int    `db:"age"`
	CityLabel     *string `db:"city_label"`
	Bio           *string `db:"bio"`
	GenderID      *string `db:"gender_id"`
	FavoriteGames *string `db:"favorite_games"`
	CreatedAt     *string `db:"created_at"`
	UpdatedAt     *string `db:"updated_at"`
}

type Preference struct {
	UserID        string  `db:"user_id"`
	AgeMin        int     `db:"age_min"`
	AgeMax        int     `db:"age_max"`
	DistanceScope string  `db:"distance_scope"`
	WhoToMeetOpen bool    `db:"who_to_meet_open"`
	WhoToMeet     *string `db:"who_to_meet"`
	CreatedAt     *string `db:"created_at"`
	UpdatedAt     *string `db:"updated_at"`
}

type Photo struct {
	ID              string  `db:"id"`
	UserID          string  `db:"user_id"`
	Path            string  `db:"path"`
	ThumbPath       *string `db:"thumb_path"`
	ModerationState string  `db:"moderation_state"`
	Blurhash        *string `db:"blurhash"`
	CreatedAt       *string `db:"created_at"`
	UpdatedAt       *string `db:"updated_at"`
}

type Block struct {
	ID        string  `db:"id"`
	BlockerID string  `db:"blocker_id"`
	BlockedID string  `db:"blocked_id"`
	CreatedAt *string `db:"created_at"`
	UpdatedAt *string `db:"updated_at"`
}

type Report struct {
	ID         string  `db:"id"`
	ReporterID string  `db:"reporter_id"`
	SubjectID  *string `db:"subject_id"`
	Reason     string  `db:"reason"`
	Details    *string `db:"details"`
	Status     string  `db:"status"`
	CreatedAt  *string `db:"created_at"`
	UpdatedAt  *string `db:"updated_at"`
}

type LegalHold struct {
	ID         string  `db:"id"`
	UserID     string  `db:"user_id"`
	ReportID   *string `db:"report_id"`
	Reason     string  `db:"reason"`
	Status     string  `db:"status"`
	PhotoKeys  *string `db:"photo_keys"`
	MessageIDs *string `db:"message_ids"`
	PurgeAfter *string `db:"purge_after"`
	CreatedAt  *string `db:"created_at"`
	UpdatedAt  *string `db:"updated_at"`
}

type Prompt struct {
	ID        string  `db:"id"`
	GameKind  string  `db:"game_kind"`
	Locale    string  `db:"locale"`
	Payload   string  `db:"payload"`
	Tags      *string `db:"tags"`
	Active    bool    `db:"active"`
	NsfwLevel int     `db:"nsfw_level"`
}

type GameSession struct {
	ID           string  `db:"id"`
	Kind         string  `db:"kind"`
	Mode         string  `db:"mode"`
	State        string  `db:"state"`
	StartedAt    *string `db:"started_at"`
	EndedAt      *string `db:"ended_at"`
	Config       *string `db:"config"`
	CurrentRound int     `db:"current_round"`
	AnswerBy     *string `db:"answer_by"`
	ForfeitAfter *string `db:"forfeit_after"`
	CreatedAt    *string `db:"created_at"`
	UpdatedAt    *string `db:"updated_at"`
}

type Participant struct {
	SessionID  string  `db:"session_id"`
	UserID     string  `db:"user_id"`
	Seat       int     `db:"seat"`
	JoinedAt   *string `db:"joined_at"`
	LastPollAt *string `db:"last_poll_at"`
}

type Round struct {
	ID         string  `db:"id"`
	SessionID  string  `db:"session_id"`
	RoundIndex int     `db:"round_index"`
	PromptID   *string `db:"prompt_id"`
	State      string  `db:"state"`
	AnswerBy   *string `db:"answer_by"`
	Extra      *string `db:"extra"`
}

type RoundAnswer struct {
	RoundID     string  `db:"round_id"`
	UserID      string  `db:"user_id"`
	Payload     string  `db:"payload"`
	SubmittedAt *string `db:"submitted_at"`
}

type QueueEntry struct {
	ID               string  `db:"id"`
	UserID           string  `db:"user_id"`
	MetroID          *string `db:"metro_id"`
	GameKind         string  `db:"game_kind"`
	EnqueuedAt       string  `db:"enqueued_at"`
	AllowPractice    bool    `db:"allow_practice"`
	MatchedSessionID *string `db:"matched_session_id"`
}

type Pair struct {
	ID               string  `db:"id"`
	UserA            string  `db:"user_a"`
	UserB            string  `db:"user_b"`
	State            string  `db:"state"`
	AAction          string  `db:"a_action"`
	BAction          string  `db:"b_action"`
	PendingExpiresAt *string `db:"pending_expires_at"`
	CooldownUntil    *string `db:"cooldown_until"`
	OriginSessionID  *string `db:"origin_session_id"`
	CreatedAt        *string `db:"created_at"`
	UpdatedAt        *string `db:"updated_at"`
}

type Match struct {
	ID              string  `db:"id"`
	UserA           string  `db:"user_a"`
	UserB           string  `db:"user_b"`
	State           string  `db:"state"`
	OriginSessionID *string `db:"origin_session_id"`
	UnmatchedBy     *string `db:"unmatched_by"`
	MatchedAt       *string `db:"matched_at"`
	ExpiresAt       *string `db:"expires_at"`
	CreatedAt       *string `db:"created_at"`
	UpdatedAt       *string `db:"updated_at"`
}

type Thread struct {
	ID        string  `db:"id"`
	MatchID   string  `db:"match_id"`
	State     string  `db:"state"`
	CreatedAt *string `db:"created_at"`
	UpdatedAt *string `db:"updated_at"`
}

type Message struct {
	ID        string  `db:"id"`
	ThreadID  string  `db:"thread_id"`
	SenderID  *string `db:"sender_id"`
	Kind      string  `db:"kind"`
	Body      string  `db:"body"`
	Meta      *string `db:"meta"`
	CreatedAt *string `db:"created_at"`
	UpdatedAt *string `db:"updated_at"`
}

type Invite struct {
	ID         string  `db:"id"`
	FromUserID string  `db:"from_user_id"`
	ToUserID   string  `db:"to_user_id"`
	GameKind   string  `db:"game_kind"`
	State      string  `db:"state"`
	ExpiresAt  string  `db:"expires_at"`
	CreatedAt  *string `db:"created_at"`
	UpdatedAt  *string `db:"updated_at"`
}

type SignalEvent struct {
	ID        string  `db:"id"`
	UserID    string  `db:"user_id"`
	SessionID *string `db:"session_id"`
	Kind      string  `db:"kind"`
	Key       string  `db:"key"`
	Value     *string `db:"value"`
	CreatedAt *string `db:"created_at"`
	UpdatedAt *string `db:"updated_at"`
}

type Snapshot struct {
	ID            string  `db:"id"`
	SessionID     string  `db:"session_id"`
	ScorerVersion string  `db:"scorer_version"`
	UserA         string  `db:"user_a"`
	UserB         string  `db:"user_b"`
	Score         float64 `db:"score"`
	Components    *string `db:"components"`
	Reasons       *string `db:"reasons"`
	ComputedAt    string  `db:"computed_at"`
}

type XpEvent struct {
	ID        string  `db:"id"`
	UserID    string  `db:"user_id"`
	Kind      string  `db:"kind"`
	Amount    int     `db:"amount"`
	CreatedAt *string `db:"created_at"`
	UpdatedAt *string `db:"updated_at"`
}

type AppSession struct {
	ID         string  `db:"id"`
	UserID     string  `db:"user_id"`
	CreatedAt  string  `db:"created_at"`
	ExpiresAt  string  `db:"expires_at"`
	LastSeenAt *string `db:"last_seen_at"`
}

type OTPCode struct {
	Phone     string `db:"phone"`
	Code      string `db:"code"`
	ExpiresAt string `db:"expires_at"`
}

type FeatureFlag struct {
	Key   string `db:"key"`
	Value string `db:"value"`
}
