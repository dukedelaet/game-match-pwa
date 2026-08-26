<?php

namespace App\Support;

use App\Models\CompatibilitySnapshot;
use App\Models\GameSession;
use App\Models\RoundAnswer;
use App\Models\SignalEvent;
use App\Models\User;
use App\Models\UserTrait;
use Illuminate\Support\Str;

class Scorer
{
    public static function scoreSession(GameSession $session): ?CompatibilitySnapshot
    {
        if ($session->mode === 'practice') {
            return null;
        }
        $ids = $session->participants()->pluck('user_id')->all();
        if (count($ids) !== 2) {
            return null;
        }
        [$a, $b] = Safety::orderedPair($ids[0], $ids[1]);
        $ua = User::with(['profile', 'intents'])->find($a);
        $ub = User::with(['profile', 'intents'])->find($b);
        if (! $ua || ! $ub) {
            return null;
        }

        $ta = UserTrait::where('user_id', $a)->pluck('trait_id')->all();
        $tb = UserTrait::where('user_id', $b)->pluck('trait_id')->all();
        $jTraits = self::jaccard($ta, $tb);

        $ia = $ua->intents->pluck('intent')->all();
        $ib = $ub->intents->pluck('intent')->all();
        $jInt = self::jaccard($ia, $ib);

        $same = 0;
        $n = 0;
        $rounds = $session->rounds()->with('answers')->get();
        foreach ($rounds as $round) {
            $ans = $round->answers;
            if ($ans->count() < 2) {
                continue;
            }
            $n++;
            $p0 = json_encode($ans[0]->payload);
            $p1 = json_encode($ans[1]->payload);
            if ($ans[0]->payload == $ans[1]->payload) {
                $same++;
            }
            unset($p0, $p1);
        }
        $behavior = $n ? $same / $n : 0.5;
        $loc = $ua->metro_id && $ua->metro_id === $ub->metro_id ? 0.6 : 0.0;

        $wP = 0.3125;
        $wI = 0.25;
        $wL = 0.1875;
        $wB = 0.1875;
        $wG = 0.0625;
        $score = $wP * $jTraits + $wI * $jInt + $wL * $jTraits + $wB * $behavior + $wG * $loc;
        $score = max(0, min(1, $score));

        $reasons = [];
        if ($jTraits >= 0.3) {
            $reasons[] = 'Similar vibe';
        }
        if ($behavior >= 0.5) {
            $reasons[] = 'Same game energy';
        }
        if ($jInt >= 0.5) {
            $reasons[] = 'Same reasons for being here';
        }
        if ($loc > 0) {
            $reasons[] = 'Same city';
        }
        $reasons = array_slice($reasons ?: ['Still getting to know you'], 0, 3);

        $percent = $n >= 6 ? (int) round($score * 100) : null;

        foreach ([$a, $b] as $uid) {
            foreach ($rounds as $round) {
                $ans = $round->answers->firstWhere('user_id', $uid);
                if (! $ans) {
                    continue;
                }
                SignalEvent::create([
                    'id' => (string) Str::uuid(),
                    'user_id' => $uid,
                    'session_id' => $session->id,
                    'kind' => 'choice',
                    'key' => 'round.'.$round->index,
                    'value' => $ans->payload,
                ]);
            }
        }

        return CompatibilitySnapshot::create([
            'id' => (string) Str::uuid(),
            'session_id' => $session->id,
            'scorer_version' => 'scorer_v0',
            'user_a' => $a,
            'user_b' => $b,
            'score' => $score,
            'components' => [
                'personality' => $jTraits,
                'interests' => $jInt,
                'lifestyle' => $jTraits,
                'behavior' => $behavior,
                'location' => $loc,
                'percent' => $percent,
            ],
            'reasons' => $reasons,
            'computed_at' => now(),
        ]);
    }

    private static function jaccard(array $a, array $b): float
    {
        if ($a === [] && $b === []) {
            return 0.5;
        }
        $inter = count(array_intersect($a, $b));
        $union = count(array_unique(array_merge($a, $b)));

        return $union ? $inter / $union : 0.0;
    }
}
