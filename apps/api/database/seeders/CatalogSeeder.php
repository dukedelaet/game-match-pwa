<?php

namespace Database\Seeders;

use App\Models\FeatureFlag;
use App\Models\Gender;
use App\Models\Metro;
use App\Models\Prompt;
use App\Models\TraitItem;
use App\Models\User;
use Illuminate\Database\Seeder;
use Illuminate\Support\Str;

class CatalogSeeder extends Seeder
{
    public function run(): void
    {
        $house = config('gamematch.house_user_id');

        $la = Metro::updateOrCreate(
            ['slug' => 'los-angeles'],
            [
                'label' => 'Los Angeles',
                'centroid_lat' => 34.05,
                'centroid_lng' => -118.24,
                'adjacent_ids' => [],
            ]
        );
        Metro::updateOrCreate(
            ['slug' => 'new-york'],
            [
                'label' => 'New York',
                'centroid_lat' => 40.71,
                'centroid_lng' => -74.00,
                'adjacent_ids' => [],
            ]
        );

        foreach ([['woman', 'Woman'], ['man', 'Man'], ['non-binary', 'Non-binary'], ['prefer_not', 'Prefer not to say']] as $i => $g) {
            Gender::updateOrCreate(
                ['slug' => $g[0]],
                ['label' => $g[1], 'sort' => $i, 'active' => true]
            );
        }

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
            TraitItem::updateOrCreate(
                ['slug' => $t[0]],
                ['label' => $t[1], 'emoji' => $t[2], 'sort' => $i]
            );
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
        foreach ($tot as $i => $row) {
            Prompt::updateOrCreate(
                ['id' => $this->stableId('tot-'.$i)],
                [
                    'game_kind' => 'this_or_that',
                    'payload' => [
                        'left' => ['id' => 'left', 'label' => $row[0], 'tags' => $row[2]],
                        'right' => ['id' => 'right', 'label' => $row[1], 'tags' => $row[2]],
                    ],
                    'tags' => $row[2],
                    'active' => true,
                ]
            );
        }

        $q = [
            ['Ideal first date?', ['Dinner', 'Walk', 'Arcade', 'Something spontaneous']],
            ['$10,000 first buy?', ['Travel', 'Motorcycle', 'Save it', 'Party']],
            ['Weekend vibe?', ['Hike', 'Brunch', 'Couch', 'Club']],
            ['Love language?', ['Time', 'Words', 'Gifts', 'Touch']],
            ['Karaoke song?', ['Power ballad', 'Rap', 'Sit it out', 'Duet']],
            ['Breakfast?', ['Savory', 'Sweet', 'Coffee only', 'Skip it']],
        ];
        foreach ($q as $i => $row) {
            $opts = [];
            foreach ($row[1] as $j => $label) {
                $opts[] = ['id' => chr(97 + $j), 'label' => $label, 'tags' => []];
            }
            foreach (['twenty_questions', 'guess_my_answer'] as $kind) {
                Prompt::updateOrCreate(
                    ['id' => $this->stableId($kind.'-'.$i)],
                    [
                        'game_kind' => $kind,
                        'payload' => ['question' => $row[0], 'options' => $opts],
                        'tags' => [],
                        'active' => true,
                    ]
                );
            }
        }

        FeatureFlag::updateOrCreate(['key' => 'auth.public_signup'], ['value' => ['v' => false]]);
        FeatureFlag::updateOrCreate(['key' => 'games.enabled'], ['value' => ['v' => true]]);
        FeatureFlag::updateOrCreate(['key' => 'staff.force_pair'], ['value' => ['v' => false]]);

        User::updateOrCreate(
            ['id' => $house],
            [
                'name' => 'House',
                'status' => 'active',
                'onboarding_step' => 'done',
                'role' => 'user',
                'metro_id' => $la->id,
            ]
        );
    }

    private function stableId(string $name): string
    {
        $hex = md5('gamematch-catalog:'.$name);

        return sprintf(
            '%s-%s-%s-%s-%s',
            substr($hex, 0, 8),
            substr($hex, 8, 4),
            '4'.substr($hex, 13, 3),
            '8'.substr($hex, 17, 3),
            substr($hex, 20, 12)
        );
    }
}
