<?php

namespace App\Support;

use App\Models\GameSession;
use App\Models\Metro;
use App\Models\QueueEntry;
use App\Models\SessionParticipant;
use App\Models\User;
use Illuminate\Support\Str;

class Matcher
{
    public static function enqueue(User $user, string $kind, bool $allowPractice): QueueEntry
    {
        QueueEntry::where('user_id', $user->id)->delete();

        return QueueEntry::create([
            'id' => (string) Str::uuid(),
            'user_id' => $user->id,
            'metro_id' => $user->metro_id,
            'game_kind' => $kind,
            'enqueued_at' => now(),
            'allow_practice' => $allowPractice,
        ]);
    }

    public static function tick(User $user): array
    {
        $entry = QueueEntry::where('user_id', $user->id)->first();
        if (! $entry) {
            return ['state' => 'idle'];
        }
        if ($entry->matched_session_id) {
            return ['state' => 'matched', 'sessionId' => $entry->matched_session_id, 'gameKind' => $entry->game_kind];
        }

        $waited = max(0, time() - $entry->enqueued_at->getTimestamp());
        $session = self::tryMatch($entry);
        if ($session) {
            return ['state' => 'matched', 'sessionId' => $session->id, 'gameKind' => $entry->game_kind];
        }

        $houseId = config('gamematch.house_user_id');
        if ($entry->allow_practice && $waited >= 30 && $houseId) {
            $session = GameEngine::startSession($entry->game_kind, 'practice', [$user->id, $houseId]);
            $entry->matched_session_id = $session->id;
            $entry->save();

            return ['state' => 'matched', 'sessionId' => $session->id, 'gameKind' => $entry->game_kind];
        }

        return [
            'state' => 'waiting',
            'gameKind' => $entry->game_kind,
            'positionHint' => 'searching',
            'estimatedWaitSec' => 45,
            'waitedSec' => $waited,
            'canPractice' => $waited >= 30,
        ];
    }

    private static function tryMatch(QueueEntry $entry): ?GameSession
    {
        $me = User::with(['profile', 'preferences', 'intents'])->find($entry->user_id);
        if (! $me || $me->incognito || $me->hidden || $me->status !== 'active') {
            return null;
        }

        $metroIds = [$entry->metro_id];
        $waited = max(0, time() - $entry->enqueued_at->getTimestamp());
        $metro = Metro::find($entry->metro_id);
        if ($waited >= 32 && $metro?->adjacent_ids) {
            $metroIds = array_merge($metroIds, $metro->adjacent_ids);
        }

        $candidates = QueueEntry::query()
            ->where('game_kind', $entry->game_kind)
            ->where('user_id', '!=', $entry->user_id)
            ->whereNull('matched_session_id')
            ->whereIn('metro_id', $metroIds)
            ->orderBy('enqueued_at')
            ->limit(20)
            ->get();

        $best = [];
        foreach ($candidates as $other) {
            $them = User::with(['profile', 'preferences', 'intents'])->find($other->user_id);
            if (! $them || $them->incognito || $them->hidden || $them->status !== 'active') {
                continue;
            }
            if (Safety::blockedEitherWay($me->id, $them->id)) {
                continue;
            }
            if (! Safety::intentsOk(Safety::userIntents($me), Safety::userIntents($them))) {
                continue;
            }
            if (! Safety::ageOk($me, $them) || ! Safety::genderOk($me, $them) || ! Safety::genderOk($them, $me)) {
                continue;
            }
            $best[] = $other;
            if (count($best) >= 5) {
                break;
            }
        }
        if ($best === []) {
            return null;
        }
        $pick = $best[0];
        $session = GameEngine::startSession($entry->game_kind, 'queue_1v1', [$entry->user_id, $pick->user_id]);
        $entry->matched_session_id = $session->id;
        $entry->save();
        $pick->matched_session_id = $session->id;
        $pick->save();

        return $session;
    }

    public static function leave(string $userId): void
    {
        QueueEntry::where('user_id', $userId)->whereNull('matched_session_id')->delete();
    }
}
