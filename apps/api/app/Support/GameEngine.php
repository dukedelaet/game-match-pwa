<?php

namespace App\Support;

use App\Models\GameSession;
use App\Models\PairRelationship;
use App\Models\Prompt;
use App\Models\Round;
use App\Models\RoundAnswer;
use App\Models\SessionParticipant;
use App\Models\User;
use App\Models\XpEvent;
use Illuminate\Support\Str;

class GameEngine
{
    public static function startSession(string $kind, string $mode, array $userIds): GameSession
    {
        $rounds = match ($kind) {
            'this_or_that' => 8,
            'twenty_questions' => 6,
            'guess_my_answer' => 6,
            default => 8,
        };
        $session = GameSession::create([
            'id' => (string) Str::uuid(),
            'kind' => $kind,
            'mode' => $mode,
            'state' => 'pending',
            'config' => ['rounds' => $rounds, 'countdown_ms' => 3000, 'reveal_ms' => 4000],
            'current_round' => 0,
        ]);
        foreach (array_values($userIds) as $i => $uid) {
            SessionParticipant::create([
                'session_id' => $session->id,
                'user_id' => $uid,
                'seat' => $i,
                'joined_at' => null,
                'last_poll_at' => now(),
            ]);
        }

        return $session;
    }

    public static function join(GameSession $session, User $user): void
    {
        $p = SessionParticipant::where('session_id', $session->id)->where('user_id', $user->id)->first();
        if (! $p) {
            abort(response()->json(['error' => ['code' => 'forbidden', 'message' => 'Not in session']], 403));
        }
        SessionParticipant::where('session_id', $session->id)->where('user_id', $user->id)->update([
            'joined_at' => now(),
            'last_poll_at' => now(),
        ]);

        $needed = $session->mode === 'practice' ? 1 : 2;
        $joined = SessionParticipant::where('session_id', $session->id)->whereNotNull('joined_at')->count();
        if ($session->state === 'pending' && $joined >= $needed) {
            self::beginCountdown($session);
        }
    }

    public static function heartbeat(GameSession $session, User $user): void
    {
        SessionParticipant::where('session_id', $session->id)->where('user_id', $user->id)
            ->update(['last_poll_at' => now()]);
        self::advance($session->fresh(['rounds.answers', 'participants']));
    }

    public static function answer(GameSession $session, User $user, array $payload): void
    {
        self::advance($session);
        $session->refresh();
        if ($session->state !== 'in_round') {
            abort(response()->json(['error' => ['code' => 'closed', 'message' => 'Not accepting answers']], 409));
        }
        $round = $session->rounds()->where('index', $session->current_round)->first();
        if (! $round) {
            abort(404);
        }
        RoundAnswer::updateOrCreate(
            ['round_id' => $round->id, 'user_id' => $user->id],
            ['payload' => $payload, 'submitted_at' => now()]
        );
        if ($session->mode === 'practice') {
            $house = config('gamematch.house_user_id');
            $prompt = $round->prompt()->first();
            $housePayload = self::houseAnswer($session->kind, $prompt?->payload ?? []);
            RoundAnswer::updateOrCreate(
                ['round_id' => $round->id, 'user_id' => $house],
                ['payload' => $housePayload, 'submitted_at' => now()]
            );
        }
        self::advance($session->fresh(['rounds.answers', 'participants']));
    }

    public static function advance(GameSession $session): void
    {
        if (in_array($session->state, ['completed', 'forfeit', 'cancelled'], true)) {
            return;
        }

        if ($session->mode !== 'practice') {
            $stale = SessionParticipant::where('session_id', $session->id)
                ->where('last_poll_at', '<', now()->subSeconds(15))
                ->exists();
            if ($stale && in_array($session->state, ['countdown', 'in_round', 'reveal_round', 'scoring'], true)) {
                $session->state = 'forfeit';
                $session->ended_at = now();
                $session->save();

                return;
            }
        }

        if ($session->state === 'pending' && $session->created_at->lt(now()->subSeconds(30))) {
            $session->state = 'cancelled';
            $session->ended_at = now();
            $session->save();

            return;
        }

        if ($session->state === 'countdown' && $session->started_at && $session->started_at->lte(now())) {
            self::openRound($session, 1);
        }

        if ($session->state === 'in_round') {
            $round = $session->rounds()->where('index', $session->current_round)->first();
            if (! $round) {
                return;
            }
            $need = $session->mode === 'practice' ? 2 : 2;
            $got = $round->answers()->count();
            $timedOut = $round->answer_by && $round->answer_by->lte(now());
            if ($got >= $need || $timedOut) {
                $round->state = 'reveal';
                $round->save();
                $session->state = 'reveal_round';
                $session->answer_by = now()->addSeconds(4);
                $session->save();
            }
        }

        if ($session->state === 'reveal_round' && $session->answer_by && $session->answer_by->lte(now())) {
            $total = $session->config['rounds'] ?? 8;
            if ($session->current_round >= $total) {
                self::complete($session);
            } else {
                self::openRound($session, $session->current_round + 1);
            }
        }
    }

    private static function beginCountdown(GameSession $session): void
    {
        $session->state = 'countdown';
        $session->started_at = now()->addSeconds(3);
        $session->save();
    }

    private static function openRound(GameSession $session, int $index): void
    {
        $prompt = Prompt::where('game_kind', $session->kind)->where('active', true)
            ->inRandomOrder()->first();
        $extra = [];
        if ($session->kind === 'guess_my_answer') {
            $seats = SessionParticipant::where('session_id', $session->id)->orderBy('seat')->get();
            $answerer = $seats[$index % max(1, $seats->count())] ?? $seats->first();
            $guesser = $seats->first(fn ($p) => $p->user_id !== $answerer?->user_id) ?? $answerer;
            $extra = [
                'answerer' => $answerer?->user_id,
                'guesser' => $guesser?->user_id,
            ];
        }
        $round = Round::firstOrCreate(
            ['session_id' => $session->id, 'index' => $index],
            [
                'id' => (string) Str::uuid(),
                'prompt_id' => $prompt?->id,
                'state' => 'open',
                'answer_by' => now()->addSeconds($session->kind === 'this_or_that' ? 8 : 12),
                'extra' => $extra,
            ]
        );
        $session->state = 'in_round';
        $session->current_round = $index;
        $session->answer_by = $round->answer_by;
        $session->save();
    }

    private static function complete(GameSession $session): void
    {
        $session->state = 'scoring';
        $session->save();
        $snap = Scorer::scoreSession($session->fresh(['rounds.answers', 'participants']));
        $session->state = 'completed';
        $session->ended_at = now();
        $session->save();

        $ids = $session->participants()->pluck('user_id')->all();
        $house = config('gamematch.house_user_id');
        foreach ($ids as $uid) {
            if ($uid === $house) {
                continue;
            }
            $amount = $session->mode === 'practice' ? 5 : 50;
            XpEvent::create(['id' => (string) Str::uuid(), 'user_id' => $uid, 'kind' => $session->mode === 'practice' ? 'practice_complete' : 'session_complete', 'amount' => $amount]);
            $u = User::find($uid);
            if ($u) {
                $u->xp = $u->xp + $amount;
                $u->level = self::levelForXp($u->xp);
                $u->save();
            }
        }

        if ($session->mode !== 'practice' && count($ids) === 2) {
            [$a, $b] = Safety::orderedPair($ids[0], $ids[1]);
            $pair = PairRelationship::firstOrNew(['user_a' => $a, 'user_b' => $b]);
            if (! $pair->exists) {
                $pair->id = (string) Str::uuid();
            }
            $cd = $pair->cooldown_until;
            if ($cd && $cd->isFuture() && in_array($pair->state, ['closed', 'unmatched', 'blocked'], true)) {
                // still cooling
            } else {
                $pair->state = 'open_play';
                $pair->a_action = 'none';
                $pair->b_action = 'none';
                $pair->pending_expires_at = null;
                $pair->origin_session_id = $session->id;
                $pair->save();
            }
        }
        unset($snap);
    }

    public static function levelForXp(int $xp): int
    {
        $level = 1;
        while (100 * $level * ($level + 1) / 2 <= $xp) {
            $level++;
            if ($level > 99) {
                break;
            }
        }

        return $level;
    }

    private static function houseAnswer(string $kind, array $payload): array
    {
        if ($kind === 'this_or_that') {
            return ['choice' => 'left'];
        }

        return ['optionId' => $payload['options'][0]['id'] ?? 'a'];
    }

    public static function view(GameSession $session, User $user): array
    {
        self::advance($session->fresh(['rounds.answers', 'participants', 'rounds.prompt']));
        $session = $session->fresh(['rounds.answers', 'participants', 'rounds.prompt']);
        $seat = (int) SessionParticipant::where('session_id', $session->id)->where('user_id', $user->id)->value('seat');
        $others = $session->participants->where('user_id', '!=', $user->id);
        $oppId = $others->first()?->user_id;
        $isHouse = $oppId && $oppId === config('gamematch.house_user_id');

        $round = $session->rounds->firstWhere('index', $session->current_round);
        $roundView = null;
        $reveal = null;
        if ($round && in_array($session->state, ['in_round', 'reveal_round'], true)) {
            $prompt = $round->prompt;
            $you = $round->answers->firstWhere('user_id', $user->id);
            $them = $oppId ? $round->answers->firstWhere('user_id', $oppId) : null;
            $yourRole = null;
            if ($session->kind === 'guess_my_answer' && is_array($round->extra)) {
                $yourRole = ($round->extra['answerer'] ?? null) === $user->id ? 'answerer' : 'guesser';
            }
            $roundView = [
                'index' => $round->index,
                'total' => $session->config['rounds'] ?? 8,
                'kind' => $session->kind,
                'prompt' => $prompt?->payload,
                'yourRole' => $yourRole,
                'deadline' => [
                    'answerBy' => optional($round->answer_by)?->toIso8601String(),
                    'serverNow' => now()->toIso8601String(),
                ],
                'youSubmitted' => (bool) $you,
                'opponentSubmitted' => (bool) $them,
            ];
            if ($session->state === 'reveal_round') {
                $same = $you && $them && $you->payload == $them->payload;
                if ($session->kind === 'guess_my_answer' && is_array($round->extra)) {
                    $ans = $round->answers->firstWhere('user_id', $round->extra['answerer'] ?? '');
                    $guess = $round->answers->firstWhere('user_id', $round->extra['guesser'] ?? '');
                    $same = ($ans?->payload['optionId'] ?? null) === ($guess?->payload['optionId'] ?? null);
                }
                $reveal = [
                    'index' => $round->index,
                    'kind' => $session->kind,
                    'you' => ['payload' => $you?->payload],
                    'opponent' => ['payload' => $them?->payload],
                    'same' => $same,
                ];
            }
        }

        $completed = null;
        if ($session->state === 'completed') {
            $snap = $session->id ? \App\Models\CompatibilitySnapshot::where('session_id', $session->id)->first() : null;
            $pair = null;
            $pairId = null;
            if ($oppId && ! $isHouse) {
                [$a, $b] = Safety::orderedPair($user->id, $oppId);
                $pr = PairRelationship::where('user_a', $a)->where('user_b', $b)->first();
                $pairId = $pr?->id;
                $your = $pr ? ($user->id === $pr->user_a ? $pr->a_action : $pr->b_action) : 'none';
                $their = $pr ? ($user->id === $pr->user_a ? $pr->b_action : $pr->a_action) : 'none';
                $pair = $pr ? [
                    'id' => $pr->id,
                    'state' => $pr->state,
                    'yourAction' => $your,
                    'theyConnected' => $their === 'connect',
                ] : null;
            }
            $completed = [
                'sessionId' => $session->id,
                'pairId' => $pairId,
                'connectEligible' => $session->mode !== 'practice' && $session->state === 'completed',
                'rematchUntil' => $session->ended_at?->copy()->addSeconds(30)->toIso8601String(),
                'snapshot' => $snap ? [
                    'score' => $snap->score,
                    'percent' => $snap->components['percent'] ?? null,
                    'reasons' => $snap->reasons,
                    'components' => $snap->components,
                ] : null,
                'pair' => $pair,
                'practiceOpponent' => $isHouse ? 'House' : null,
            ];
        }

        $opp = null;
        if ($oppId && ! $isHouse) {
            $ou = User::with('profile')->find($oppId);
            $opp = $ou ? ['id' => $ou->id, 'name' => $ou->name, 'age' => $ou->profile?->age] : null;
        }
        if ($isHouse) {
            $opp = ['id' => 'house', 'name' => 'House', 'age' => null];
        }

        return [
            'sessionId' => $session->id,
            'state' => $session->state,
            'kind' => $session->kind,
            'mode' => $session->mode,
            'youSeat' => $seat,
            'resumeToken' => $session->id,
            'opponent' => $opp,
            'round' => $roundView,
            'reveal' => $reveal,
            'completed' => $completed,
            'serverNow' => now()->toIso8601String(),
        ];
    }
}
