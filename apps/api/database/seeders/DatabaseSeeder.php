<?php

namespace Database\Seeders;

use App\Models\FeatureFlag;
use App\Models\Gender;
use App\Models\Metro;
use App\Models\Preference;
use App\Models\Profile;
use App\Models\Prompt;
use App\Models\TraitItem;
use App\Models\User;
use App\Models\UserIntent;
use App\Models\UserTrait;
use Illuminate\Database\Seeder;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;

class DatabaseSeeder extends Seeder
{
    public function run(): void
    {
        $house = config('gamematch.house_user_id');

        $la = Metro::create([
            'id' => (string) Str::uuid(),
            'slug' => 'los-angeles',
            'label' => 'Los Angeles',
            'centroid_lat' => 34.05,
            'centroid_lng' => -118.24,
            'adjacent_ids' => [],
        ]);
        Metro::create([
            'id' => (string) Str::uuid(),
            'slug' => 'new-york',
            'label' => 'New York',
            'centroid_lat' => 40.71,
            'centroid_lng' => -74.00,
            'adjacent_ids' => [],
        ]);

        $genders = [];
        foreach ([['woman', 'Woman'], ['man', 'Man'], ['non-binary', 'Non-binary'], ['prefer_not', 'Prefer not to say']] as $i => $g) {
            $genders[$g[0]] = Gender::create(['id' => (string) Str::uuid(), 'slug' => $g[0], 'label' => $g[1], 'sort' => $i, 'active' => true]);
        }

        $traits = [];
        $traitSeed = [
            ['competitive', 'Competitive', '🔥'],
            ['funny', 'Funny', '😂'],
            ['creative', 'Creative', '🎨'],
            ['night-owl', 'Night owl', '🌙'],
            ['music', 'Music lover', '🎵'],
            ['active', 'Active', '🏋️'],
            ['social', 'Social', '🍻'],
            ['nerdy', 'Nerdy', '🧠'],
            ['romantic', 'Romantic', '❤️'],
            ['chaotic', 'Chaotic', '😈'],
        ];
        foreach ($traitSeed as $i => $t) {
            $traits[$t[0]] = TraitItem::create(['id' => (string) Str::uuid(), 'slug' => $t[0], 'label' => $t[1], 'emoji' => $t[2], 'sort' => $i]);
        }

        $tot = [
            ['Beach', 'Mountains', ['interest.outdoors']],
            ['Tacos', 'Sushi', ['interest.food']],
            ['Stay in', 'Go out', ['lifestyle.social']],
            ['Risk it', 'Play it safe', ['behavior.risk']],
            ['Early night', 'Sunrise', ['lifestyle.schedule']],
            ['Cats', 'Dogs', ['interest.pets']],
            ['Texts', 'Calls', ['lifestyle.chat']],
            ['Concert', 'Museum', ['interest.culture']],
            ['Coffee', 'Cocktails', ['interest.drinks']],
            ['Board games', 'Video games', ['interest.games']],
        ];
        foreach ($tot as $row) {
            Prompt::create([
                'id' => (string) Str::uuid(),
                'game_kind' => 'this_or_that',
                'payload' => [
                    'left' => ['id' => 'left', 'label' => $row[0], 'tags' => $row[2]],
                    'right' => ['id' => 'right', 'label' => $row[1], 'tags' => $row[2]],
                ],
                'tags' => $row[2],
                'active' => true,
            ]);
        }

        $q = [
            ['Ideal first date?', ['Dinner', 'Walk', 'Arcade', 'Something spontaneous']],
            ['$10,000 first buy?', ['Travel', 'Motorcycle', 'Save it', 'Party']],
            ['Weekend vibe?', ['Hike', 'Brunch', 'Couch', 'Club']],
            ['Love language?', ['Time', 'Words', 'Gifts', 'Touch']],
            ['Karaoke song?', ['Power ballad', 'Rap', 'Sit it out', 'Duet']],
            ['Breakfast?', ['Savory', 'Sweet', 'Coffee only', 'Skip it']],
        ];
        foreach ($q as $row) {
            $opts = [];
            foreach ($row[1] as $i => $label) {
                $opts[] = ['id' => chr(97 + $i), 'label' => $label, 'tags' => []];
            }
            Prompt::create([
                'id' => (string) Str::uuid(),
                'game_kind' => 'twenty_questions',
                'payload' => ['question' => $row[0], 'options' => $opts],
                'tags' => [],
                'active' => true,
            ]);
            Prompt::create([
                'id' => (string) Str::uuid(),
                'game_kind' => 'guess_my_answer',
                'payload' => ['question' => $row[0], 'options' => $opts],
                'tags' => [],
                'active' => true,
            ]);
        }

        FeatureFlag::insert([
            ['key' => 'auth.public_signup', 'value' => json_encode(['v' => true])],
            ['key' => 'games.enabled', 'value' => json_encode(['v' => true])],
            ['key' => 'staff.force_pair', 'value' => json_encode(['v' => true])],
        ]);

        User::create([
            'id' => $house,
            'name' => 'House',
            'status' => 'active',
            'onboarding_step' => 'done',
            'role' => 'user',
            'metro_id' => $la->id,
        ]);

        $this->demoUser('Alex', '+15551111111', $la->id, $genders['woman']->id, $traits, ['dating', 'gaming']);
        $this->demoUser('Jordan', '+15552222222', $la->id, $genders['man']->id, $traits, ['dating', 'socializing']);

        foreach (['+15551111111', '+15552222222', '+15550000000'] as $phone) {
            DB::table('allowlist_phones')->insert([
                'phone_e164_hash' => hash('sha256', $phone),
                'created_at' => now(),
                'updated_at' => now(),
            ]);
        }

        User::create([
            'id' => (string) Str::uuid(),
            'name' => 'Staff',
            'phone_e164_hash' => hash('sha256', '+15550000000'),
            'status' => 'active',
            'onboarding_step' => 'done',
            'role' => 'admin',
            'metro_id' => $la->id,
            'age_attested_at' => now(),
        ]);
    }

    private function demoUser(string $name, string $phone, string $metro, string $gender, array $traits, array $intents): void
    {
        $id = (string) Str::uuid();
        $u = User::create([
            'id' => $id,
            'name' => $name,
            'phone_e164_hash' => hash('sha256', $phone),
            'status' => 'active',
            'onboarding_step' => 'done',
            'age_attested_at' => now(),
            'dob' => now()->subYears(28)->toDateString(),
            'metro_id' => $metro,
            'xp' => 40,
        ]);
        DB::table('user_private')->insert(['user_id' => $id, 'phone_e164' => $phone, 'created_at' => now(), 'updated_at' => now()]);
        Profile::create(['user_id' => $id, 'age' => 28, 'city_label' => 'Los Angeles', 'bio' => "I'm usually up for a round of something.", 'gender_id' => $gender]);
        Preference::create(['user_id' => $id, 'age_min' => 21, 'age_max' => 40, 'who_to_meet_open' => true]);
        foreach ($intents as $i) {
            UserIntent::create(['user_id' => $id, 'intent' => $i]);
        }
        $take = array_slice(array_values($traits), 0, 6);
        foreach ($take as $t) {
            UserTrait::create(['user_id' => $id, 'trait_id' => $t->id]);
        }
        unset($u);
    }
}
