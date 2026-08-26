<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Model;

class Metro extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['slug', 'label', 'centroid_lat', 'centroid_lng', 'adjacent_ids'];

    protected $hidden = ['centroid_lat', 'centroid_lng', 'adjacent_ids'];

    protected function casts(): array
    {
        return ['adjacent_ids' => 'array'];
    }
}

class Gender extends Model
{
    use HasUuids;

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = ['slug', 'label', 'sort', 'active'];
}

class TraitItem extends Model
{
    use HasUuids;

    protected $table = 'traits';

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = ['slug', 'label', 'emoji', 'sort'];
}

class Profile extends Model
{
    protected $primaryKey = 'user_id';

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'age', 'city_label', 'bio', 'gender_id', 'favorite_games'];

    protected function casts(): array
    {
        return ['favorite_games' => 'array', 'age' => 'integer'];
    }
}

class Preference extends Model
{
    protected $primaryKey = 'user_id';

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'age_min', 'age_max', 'distance_scope', 'who_to_meet_open', 'who_to_meet'];

    protected function casts(): array
    {
        return [
            'who_to_meet_open' => 'boolean',
            'who_to_meet' => 'array',
            'age_min' => 'integer',
            'age_max' => 'integer',
        ];
    }
}

class UserIntent extends Model
{
    public $timestamps = false;

    public $incrementing = false;

    protected $fillable = ['user_id', 'intent'];
}

class UserTrait extends Model
{
    public $timestamps = false;

    public $incrementing = false;

    protected $fillable = ['user_id', 'trait_id'];
}

class Photo extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'path', 'thumb_path', 'moderation_state', 'blurhash'];
}

class Block extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['blocker_id', 'blocked_id'];
}

class Report extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['reporter_id', 'subject_id', 'reason', 'details', 'status'];
}

class LegalHold extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'report_id', 'reason', 'status', 'photo_keys', 'message_ids', 'purge_after'];

    protected function casts(): array
    {
        return ['photo_keys' => 'array', 'message_ids' => 'array', 'purge_after' => 'datetime'];
    }
}

class FeatureFlag extends Model
{
    protected $primaryKey = 'key';

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = ['key', 'value'];

    protected function casts(): array
    {
        return ['value' => 'array'];
    }
}

class Prompt extends Model
{
    use HasUuids;

    protected $table = 'prompt_bank';

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = ['game_kind', 'locale', 'payload', 'tags', 'active', 'nsfw_level'];

    protected function casts(): array
    {
        return ['payload' => 'array', 'tags' => 'array', 'active' => 'boolean'];
    }
}

class GameSession extends Model
{
    use HasUuids;

    protected $table = 'game_sessions';

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'kind', 'mode', 'state', 'started_at', 'ended_at', 'config',
        'current_round', 'answer_by', 'forfeit_after',
    ];

    protected function casts(): array
    {
        return [
            'config' => 'array',
            'started_at' => 'datetime',
            'ended_at' => 'datetime',
            'answer_by' => 'datetime',
            'forfeit_after' => 'datetime',
        ];
    }

    public function participants()
    {
        return $this->hasMany(SessionParticipant::class, 'session_id');
    }

    public function rounds()
    {
        return $this->hasMany(Round::class, 'session_id')->orderBy('index');
    }
}

class SessionParticipant extends Model
{
    public $incrementing = false;

    public $timestamps = false;

    protected $fillable = ['session_id', 'user_id', 'seat', 'joined_at', 'last_poll_at'];

    protected function casts(): array
    {
        return ['joined_at' => 'datetime', 'last_poll_at' => 'datetime'];
    }
}

class Round extends Model
{
    use HasUuids;

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = ['session_id', 'index', 'prompt_id', 'state', 'answer_by', 'extra'];

    protected function casts(): array
    {
        return ['extra' => 'array', 'answer_by' => 'datetime'];
    }

    public function answers()
    {
        return $this->hasMany(RoundAnswer::class, 'round_id');
    }

    public function prompt()
    {
        return $this->belongsTo(Prompt::class, 'prompt_id');
    }
}

class RoundAnswer extends Model
{
    public $incrementing = false;

    public $timestamps = false;

    protected $fillable = ['round_id', 'user_id', 'payload', 'submitted_at'];

    protected function casts(): array
    {
        return ['payload' => 'array', 'submitted_at' => 'datetime'];
    }
}

class QueueEntry extends Model
{
    use HasUuids;

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'metro_id', 'game_kind', 'enqueued_at', 'allow_practice', 'matched_session_id'];

    protected function casts(): array
    {
        return ['enqueued_at' => 'datetime', 'allow_practice' => 'boolean'];
    }
}

class PairRelationship extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'user_a', 'user_b', 'state', 'a_action', 'b_action',
        'pending_expires_at', 'cooldown_until', 'origin_session_id',
    ];

    protected function casts(): array
    {
        return ['pending_expires_at' => 'datetime', 'cooldown_until' => 'datetime'];
    }
}

class UserMatch extends Model
{
    use HasUuids;

    protected $table = 'matches';

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'user_a', 'user_b', 'state', 'origin_session_id', 'unmatched_by', 'matched_at', 'expires_at',
    ];

    protected function casts(): array
    {
        return ['matched_at' => 'datetime', 'expires_at' => 'datetime'];
    }
}

class ChatThread extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['match_id', 'state'];

    public function messages()
    {
        return $this->hasMany(Message::class, 'thread_id');
    }
}

class Message extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['thread_id', 'sender_id', 'kind', 'body', 'meta'];

    protected function casts(): array
    {
        return ['meta' => 'array'];
    }
}

class Invite extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['from_user_id', 'to_user_id', 'game_kind', 'state', 'expires_at'];

    protected function casts(): array
    {
        return ['expires_at' => 'datetime'];
    }
}

class SignalEvent extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'session_id', 'kind', 'key', 'value'];

    protected function casts(): array
    {
        return ['value' => 'array'];
    }
}

class CompatibilitySnapshot extends Model
{
    use HasUuids;

    public $incrementing = false;

    public $timestamps = false;

    protected $keyType = 'string';

    protected $fillable = [
        'session_id', 'scorer_version', 'user_a', 'user_b', 'score', 'components', 'reasons', 'computed_at',
    ];

    protected function casts(): array
    {
        return ['components' => 'array', 'reasons' => 'array', 'computed_at' => 'datetime', 'score' => 'float'];
    }
}

class XpEvent extends Model
{
    use HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = ['user_id', 'kind', 'amount'];
}
