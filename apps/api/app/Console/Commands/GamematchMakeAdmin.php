<?php

namespace App\Console\Commands;

use App\Models\Preference;
use App\Models\User;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;

class GamematchMakeAdmin extends Command
{
    protected $signature = 'gamematch:make-admin {phone}';

    protected $description = 'Allowlist a phone and set role=admin (creates the user if needed)';

    public function handle(): int
    {
        $phone = preg_replace('/\s+/', '', (string) $this->argument('phone'));
        if (! preg_match('/^\+?[0-9]{10,15}$/', $phone)) {
            $this->error('Phone must look like +15551234567');

            return self::FAILURE;
        }
        $hash = hash('sha256', $phone);
        DB::table('allowlist_phones')->updateOrInsert(
            ['phone_e164_hash' => $hash],
            ['updated_at' => now(), 'created_at' => now()]
        );
        $user = User::where('phone_e164_hash', $hash)->first();
        if (! $user) {
            $user = User::create([
                'id' => (string) Str::uuid(),
                'phone_e164_hash' => $hash,
                'name' => 'Admin',
                'status' => 'pending',
                'onboarding_step' => 'intent',
                'role' => 'admin',
                'xp' => 0,
                'level' => 1,
            ]);
            DB::table('user_private')->insert([
                'user_id' => $user->id,
                'phone_e164' => $phone,
                'created_at' => now(),
                'updated_at' => now(),
            ]);
            Preference::create(['user_id' => $user->id, 'who_to_meet_open' => true]);
            $this->info('Allowlisted and created pending admin user.');
        } else {
            $user->role = 'admin';
            $user->save();
            $this->info('Allowlisted. '.$user->name.' is admin.');
        }

        return self::SUCCESS;
    }
}
