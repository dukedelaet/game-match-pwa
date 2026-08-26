<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Foundation\Auth\User as Authenticatable;

class User extends Authenticatable
{
    use HasFactory, HasUuids;

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'name', 'email', 'phone_e164_hash', 'role', 'onboarding_step', 'status',
        'age_attested_at', 'dob', 'incognito', 'hidden', 'last_seen_at', 'last_active_on',
        'metro_id', 'approx_geohash', 'xp', 'level',
    ];

    protected $hidden = ['remember_token', 'phone_e164_hash', 'dob'];

    protected function casts(): array
    {
        return [
            'age_attested_at' => 'datetime',
            'last_seen_at' => 'datetime',
            'incognito' => 'boolean',
            'hidden' => 'boolean',
            'xp' => 'integer',
            'level' => 'integer',
        ];
    }

    public function profile()
    {
        return $this->hasOne(Profile::class);
    }

    public function preferences()
    {
        return $this->hasOne(Preference::class);
    }

    public function photos()
    {
        return $this->hasMany(Photo::class);
    }

    public function intents()
    {
        return $this->hasMany(UserIntent::class);
    }

    public function touchPresence(): void
    {
        $this->forceFill([
            'last_seen_at' => now(),
            'last_active_on' => now()->toDateString(),
        ])->save();
    }
}
