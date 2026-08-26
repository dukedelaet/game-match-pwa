<?php

namespace App\Support;

use App\Models\Block;
use App\Models\User;
use Illuminate\Support\Facades\DB;

class Safety
{
    public static function blockedEitherWay(string $a, string $b): bool
    {
        return Block::query()
            ->where(function ($q) use ($a, $b) {
                $q->where('blocker_id', $a)->where('blocked_id', $b);
            })
            ->orWhere(function ($q) use ($a, $b) {
                $q->where('blocker_id', $b)->where('blocked_id', $a);
            })
            ->exists();
    }

    public static function assertNotBlocked(string $a, string $b): void
    {
        if (self::blockedEitherWay($a, $b)) {
            abort(response()->json(['error' => ['code' => 'blocked', 'message' => 'Not found']], 404));
        }
    }

    public static function orderedPair(string $a, string $b): array
    {
        return $a < $b ? [$a, $b] : [$b, $a];
    }

    public static function intentsOk(array $a, array $b): bool
    {
        if ($a === [] || $b === []) {
            return false;
        }
        $aD = in_array('dating', $a, true);
        $bD = in_array('dating', $b, true);
        if ($aD || $bD) {
            return $aD && $bD;
        }

        return true;
    }

    public static function genderOk(User $viewer, User $other): bool
    {
        $pref = $viewer->preferences;
        if (! $pref || $pref->who_to_meet_open) {
            return true;
        }
        $wanted = $pref->who_to_meet ?? [];
        $gid = $other->profile?->gender_id;
        if (! $gid) {
            return false;
        }

        return in_array($gid, $wanted, true);
    }

    public static function userIntents(User $u): array
    {
        return $u->intents()->pluck('intent')->all();
    }

    public static function ageOk(User $a, User $b): bool
    {
        $ageA = $a->profile?->age;
        $ageB = $b->profile?->age;
        $pa = $a->preferences;
        $pb = $b->preferences;
        if (! $ageA || ! $ageB || ! $pa || ! $pb) {
            return false;
        }

        return $ageB >= $pa->age_min && $ageB <= $pa->age_max
            && $ageA >= $pb->age_min && $ageA <= $pb->age_max;
    }
}
