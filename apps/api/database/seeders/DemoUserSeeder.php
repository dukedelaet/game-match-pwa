<?php

namespace Database\Seeders;

use App\Models\FeatureFlag;
use App\Models\Gender;
use App\Models\Metro;
use App\Models\Preference;
use App\Models\Profile;
use App\Models\TraitItem;
use App\Models\User;
use App\Models\UserIntent;
use App\Models\UserTrait;
use Illuminate\Database\Seeder;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;

class DemoUserSeeder extends Seeder
{
    public function run(): void
    {
        $la = Metro::where('slug', 'los-angeles')->firstOrFail();
        $genders = Gender::query()->get()->keyBy('slug');
        $traits = TraitItem::query()->orderBy('sort')->get()->all();

        FeatureFlag::updateOrCreate(['key' => 'auth.public_signup'], ['value' => ['v' => true]]);
        FeatureFlag::updateOrCreate(['key' => 'staff.force_pair'], ['value' => ['v' => true]]);

        $this->demoUser('Alex', '+15551111111', $la->id, $genders['woman']->id, $traits, ['dating', 'gaming']);
        $this->demoUser('Jordan', '+15552222222', $la->id, $genders['man']->id, $traits, ['dating', 'socializing']);

        foreach (['+15551111111', '+15552222222', '+15550000000'] as $phone) {
            DB::table('allowlist_phones')->updateOrInsert(
                ['phone_e164_hash' => hash('sha256', $phone)],
                ['updated_at' => now(), 'created_at' => now()]
            );
        }

        User::updateOrCreate(
            ['phone_e164_hash' => hash('sha256', '+15550000000')],
            [
                'name' => 'Staff',
                'status' => 'active',
                'onboarding_step' => 'done',
                'role' => 'admin',
                'metro_id' => $la->id,
                'age_attested_at' => now(),
            ]
        );
    }

    private function demoUser(string $name, string $phone, string $metro, string $gender, array $traits, array $intents): void
    {
        $hash = hash('sha256', $phone);
        $u = User::where('phone_e164_hash', $hash)->first();
        if (! $u) {
            $u = User::create([
                'id' => (string) Str::uuid(),
                'name' => $name,
                'phone_e164_hash' => $hash,
                'status' => 'active',
                'onboarding_step' => 'done',
                'age_attested_at' => now(),
                'dob' => now()->subYears(28)->toDateString(),
                'metro_id' => $metro,
                'xp' => 40,
            ]);
        } else {
            $u->fill([
                'name' => $name,
                'status' => 'active',
                'onboarding_step' => 'done',
                'metro_id' => $metro,
            ])->save();
        }
        $id = $u->id;
        DB::table('user_private')->updateOrInsert(
            ['user_id' => $id],
            ['phone_e164' => $phone, 'updated_at' => now(), 'created_at' => now()]
        );
        Profile::updateOrCreate(
            ['user_id' => $id],
            ['age' => 28, 'city_label' => 'Los Angeles', 'bio' => "I'm usually up for a round of something.", 'gender_id' => $gender]
        );
        Preference::updateOrCreate(
            ['user_id' => $id],
            ['age_min' => 21, 'age_max' => 40, 'who_to_meet_open' => true]
        );
        UserIntent::where('user_id', $id)->delete();
        foreach ($intents as $i) {
            UserIntent::create(['user_id' => $id, 'intent' => $i]);
        }
        UserTrait::where('user_id', $id)->delete();
        foreach (array_slice($traits, 0, 6) as $t) {
            UserTrait::create(['user_id' => $id, 'trait_id' => $t->id]);
        }
    }
}
