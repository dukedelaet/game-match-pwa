<?php

namespace App\Http\Controllers;

use App\Models\Block;
use App\Models\ChatThread;
use App\Models\FeatureFlag;
use App\Models\GameSession;
use App\Models\Gender;
use App\Models\Invite;
use App\Models\LegalHold;
use App\Models\Message;
use App\Models\Metro;
use App\Models\PairRelationship;
use App\Models\Photo;
use App\Models\Preference;
use App\Models\Profile;
use App\Models\Prompt;
use App\Models\QueueEntry;
use App\Models\Report;
use App\Models\TraitItem;
use App\Models\User;
use App\Models\UserIntent;
use App\Models\UserMatch;
use App\Models\UserTrait;
use App\Models\XpEvent;
use App\Support\Flags;
use App\Support\GameEngine;
use App\Support\Matcher;
use App\Support\Safety;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Storage;
use Illuminate\Support\Str;

class ApiController extends Controller
{
    public function health()
    {
        return ['ok' => true];
    }

    public function csrf()
    {
        return ['ok' => true];
    }

    public function otpStart(Request $request)
    {
        $phone = preg_replace('/\s+/', '', (string) $request->input('phone'));
        if (! preg_match('/^\+?[0-9]{10,15}$/', $phone)) {
            return response()->json(['ok' => true]);
        }
        if (! Flags::on('auth.public_signup')) {
            $hash = hash('sha256', $phone);
            if (! \Illuminate\Support\Facades\DB::table('allowlist_phones')->where('phone_e164_hash', $hash)->exists()) {
                return ['ok' => true];
            }
        }
        $code = config('app.debug') ? config('gamematch.dev_otp') : str_pad((string) random_int(0, 999999), 6, '0', STR_PAD_LEFT);
        Cache::put('otp:'.$phone, $code, now()->addMinutes(15));

        $out = ['ok' => true];
        if (config('app.debug')) {
            $out['devCode'] = $code;
        }

        return $out;
    }

    public function oauthDemo(Request $request, string $provider)
    {
        if (! in_array($provider, ['apple', 'google'], true)) {
            return response()->json(['error' => ['code' => 'bad', 'message' => 'Unknown sign-in']], 422);
        }
        if (! config('app.debug') && ! config('services.'.$provider.'.client_id')) {
            return response()->json(['error' => ['code' => 'oauth', 'message' => 'Sign-in is not configured yet. Use a phone code.']], 501);
        }
        $email = $provider.'-demo@gamematch.local';
        $user = User::where('email', $email)->first();
        if (! $user) {
            $user = User::create([
                'id' => (string) Str::uuid(),
                'email' => $email,
                'name' => $provider === 'apple' ? 'Apple Demo' : 'Google Demo',
                'status' => 'pending',
                'onboarding_step' => 'intent',
                'role' => 'user',
                'xp' => 0,
                'level' => 1,
            ]);
            Preference::create(['user_id' => $user->id, 'who_to_meet_open' => true]);
        }
        Auth::login($user);
        $request->session()->regenerate();

        return ['user' => $this->publicMe($user)];
    }

    public function otpVerify(Request $request)
    {
        $phone = preg_replace('/\s+/', '', (string) $request->input('phone'));
        $code = (string) $request->input('code');
        $expected = Cache::get('otp:'.$phone);
        if (! $expected || $code !== $expected) {
            return response()->json(['error' => ['code' => 'invalid', 'message' => 'That code did not match']], 401);
        }
        Cache::forget('otp:'.$phone);
        $hash = hash('sha256', $phone);
        $user = User::where('phone_e164_hash', $hash)->first();
        if (! $user) {
            $user = User::create([
                'id' => (string) Str::uuid(),
                'phone_e164_hash' => $hash,
                'status' => 'pending',
                'onboarding_step' => 'intent',
                'role' => 'user',
                'xp' => 0,
                'level' => 1,
            ]);
            \Illuminate\Support\Facades\DB::table('user_private')->insert([
                'user_id' => $user->id,
                'phone_e164' => $phone,
                'created_at' => now(),
                'updated_at' => now(),
            ]);
            Preference::create([
                'user_id' => $user->id,
                'who_to_meet_open' => true,
            ]);
        }
        Auth::login($user);
        $request->session()->regenerate();
        $user->touchPresence();

        return ['user' => $this->publicMe($user)];
    }

    public function sessionUser(Request $request)
    {
        $u = $request->user();
        if (! $u) {
            return response()->json(['error' => ['code' => 'unauth', 'message' => 'Sign in']], 401);
        }
        $u->touchPresence();

        return ['user' => $this->publicMe($u)];
    }

    public function logout(Request $request)
    {
        Auth::logout();
        $request->session()->invalidate();
        $request->session()->regenerateToken();

        return ['ok' => true];
    }

    public function catalogs()
    {
        return [
            'metros' => Metro::query()->get(['id', 'slug', 'label']),
            'genders' => Gender::where('active', true)->orderBy('sort')->get(['id', 'slug', 'label']),
            'traits' => TraitItem::orderBy('sort')->get(['id', 'slug', 'label', 'emoji']),
            'intents' => [
                ['id' => 'dating', 'label' => 'Dating'],
                ['id' => 'friendship', 'label' => 'Friendship'],
                ['id' => 'gaming', 'label' => 'Gaming'],
                ['id' => 'socializing', 'label' => 'Socializing'],
            ],
        ];
    }

    public function patchMe(Request $request)
    {
        $u = $request->user();
        $data = $request->validate([
            'name' => 'nullable|string|max:40',
            'dob' => 'nullable|date',
            'metro_id' => 'nullable|uuid',
            'bio' => 'nullable|string|max:280',
            'gender_id' => 'nullable|uuid',
            'age_attested' => 'nullable|boolean',
            'onboarding_step' => 'nullable|string',
            'intents' => 'nullable|array',
            'trait_ids' => 'nullable|array',
            'age_min' => 'nullable|integer|min:18|max:99',
            'age_max' => 'nullable|integer|min:18|max:99',
            'distance_scope' => 'nullable|in:metro,metro_and_adjacent',
            'who_to_meet_open' => 'nullable|boolean',
            'who_to_meet' => 'nullable|array',
            'incognito' => 'nullable|boolean',
            'hidden' => 'nullable|boolean',
        ]);
        if (! empty($data['name'])) {
            $u->name = $data['name'];
        }
        if (! empty($data['dob'])) {
            $u->dob = $data['dob'];
            $age = now()->diffInYears(\Carbon\Carbon::parse($data['dob']));
            if ($age < 18) {
                return response()->json(['error' => ['code' => 'age', 'message' => 'You must be 18+']], 403);
            }
        }
        if (! empty($data['metro_id'])) {
            $u->metro_id = $data['metro_id'];
        }
        if (! empty($data['age_attested'])) {
            $u->age_attested_at = now();
        }
        if (! empty($data['onboarding_step'])) {
            $u->onboarding_step = $data['onboarding_step'];
            if ($data['onboarding_step'] === 'done') {
                $u->status = 'active';
            }
        }
        if (array_key_exists('incognito', $data)) {
            $u->incognito = (bool) $data['incognito'];
        }
        if (array_key_exists('hidden', $data)) {
            $u->hidden = (bool) $data['hidden'];
        }
        $u->save();

        $profile = Profile::firstOrNew(['user_id' => $u->id]);
        if (! empty($data['bio'])) {
            $profile->bio = $data['bio'];
        }
        if (! empty($data['gender_id'])) {
            $profile->gender_id = $data['gender_id'];
        }
        if (! empty($data['dob'])) {
            $profile->age = now()->diffInYears(\Carbon\Carbon::parse($data['dob']));
        }
        if ($u->metro_id) {
            $profile->city_label = Metro::find($u->metro_id)?->label;
        }
        $profile->save();

        if (isset($data['intents'])) {
            UserIntent::where('user_id', $u->id)->delete();
            foreach ($data['intents'] as $intent) {
                UserIntent::create(['user_id' => $u->id, 'intent' => $intent]);
            }
        }
        if (isset($data['trait_ids'])) {
            UserTrait::where('user_id', $u->id)->delete();
            foreach ($data['trait_ids'] as $tid) {
                UserTrait::create(['user_id' => $u->id, 'trait_id' => $tid]);
            }
        }
        $pref = Preference::firstOrNew(['user_id' => $u->id]);
        foreach (['age_min', 'age_max', 'distance_scope', 'who_to_meet_open', 'who_to_meet'] as $k) {
            if (array_key_exists($k, $data)) {
                $pref->{$k} = $data[$k];
            }
        }
        $pref->save();

        return ['user' => $this->publicMe($u->fresh())];
    }

    public function uploadPhoto(Request $request)
    {
        $u = $request->user();
        $request->validate(['photo' => 'required|file|max:10240|mimes:jpeg,jpg,png,webp']);
        $file = $request->file('photo');
        $img = @imagecreatefromstring(file_get_contents($file->getRealPath()));
        if (! $img) {
            return response()->json(['error' => ['code' => 'bad_image', 'message' => 'Could not read that photo']], 422);
        }
        $w = imagesx($img);
        $h = imagesy($img);
        $max = 1080;
        if ($w > $max || $h > $max) {
            $scale = min($max / $w, $max / $h);
            $nw = (int) ($w * $scale);
            $nh = (int) ($h * $scale);
            $dst = imagecreatetruecolor($nw, $nh);
            imagecopyresampled($dst, $img, 0, 0, 0, 0, $nw, $nh, $w, $h);
            imagedestroy($img);
            $img = $dst;
        }
        $id = (string) Str::uuid();
        $dir = storage_path('app/photos/'.$u->id);
        if (! is_dir($dir)) {
            mkdir($dir, 0755, true);
        }
        $path = $dir.'/'.$id.'.jpg';
        imagejpeg($img, $path, 82);
        $tw = 320;
        $th = (int) (imagesy($img) * (320 / imagesx($img)));
        $thumb = imagecreatetruecolor($tw, max(1, $th));
        imagecopyresampled($thumb, $img, 0, 0, 0, 0, $tw, max(1, $th), imagesx($img), imagesy($img));
        $tpath = $dir.'/'.$id.'_t.jpg';
        imagejpeg($thumb, $tpath, 75);
        imagedestroy($img);
        imagedestroy($thumb);

        $photo = Photo::create([
            'id' => $id,
            'user_id' => $u->id,
            'path' => 'photos/'.$u->id.'/'.$id.'.jpg',
            'thumb_path' => 'photos/'.$u->id.'/'.$id.'_t.jpg',
            'moderation_state' => 'ok',
        ]);

        return ['photo' => ['id' => $photo->id, 'url' => url('/v1/photos/'.$photo->id)]];
    }

    public function photo(Request $request, string $id)
    {
        $photo = Photo::findOrFail($id);
        $user = $request->user();
        if ($photo->user_id !== $user?->id && $user && Safety::blockedEitherWay($user->id, $photo->user_id)) {
            abort(404);
        }
        $abs = storage_path('app/'.$photo->path);
        if (! is_file($abs)) {
            abort(404);
        }

        return response()->file($abs);
    }

    public function games()
    {
        return [
            'items' => [
                ['kind' => 'this_or_that', 'label' => 'This or That', 'ready' => true],
                ['kind' => 'twenty_questions', 'label' => '20 Questions', 'ready' => true],
                ['kind' => 'guess_my_answer', 'label' => 'Guess My Answer', 'ready' => true],
            ],
        ];
    }

    public function home(Request $request)
    {
        $u = $request->user();
        $depth = QueueEntry::whereNull('matched_session_id')->where('game_kind', 'this_or_that')->count();
        $invites = Invite::where('to_user_id', $u->id)->where('state', 'pending')->where('expires_at', '>', now())->get();
        $pending = PairRelationship::query()
            ->where(function ($q) use ($u) {
                $q->where('user_a', $u->id)->orWhere('user_b', $u->id);
            })
            ->where('state', 'pending')
            ->get();

        return [
            'queueDepth' => $depth,
            'invites' => $invites->map(fn ($i) => $this->inviteItem($i, $u)),
            'pendingPairs' => $pending->count(),
        ];
    }

    public function queueJoin(Request $request)
    {
        $u = $request->user();
        if ($u->incognito) {
            return response()->json(['error' => ['code' => 'incognito', 'message' => 'Turn off incognito to enter the lobby']], 409);
        }
        $kind = $request->input('gameKind', 'this_or_that');
        Matcher::enqueue($u, $kind, (bool) $request->input('allowPractice', false));

        return Matcher::tick($u);
    }

    public function queueStatus(Request $request)
    {
        return Matcher::tick($request->user());
    }

    public function queueLeave(Request $request)
    {
        Matcher::leave($request->user()->id);

        return ['ok' => true];
    }

    public function sessionJoin(Request $request, string $id)
    {
        $s = GameSession::findOrFail($id);
        GameEngine::join($s, $request->user());

        return GameEngine::view($s, $request->user());
    }

    public function sessionShow(Request $request, string $id)
    {
        $s = GameSession::findOrFail($id);
        GameEngine::heartbeat($s, $request->user());

        return GameEngine::view($s, $request->user());
    }

    public function sessionAnswer(Request $request, string $id)
    {
        $s = GameSession::findOrFail($id);
        GameEngine::heartbeat($s, $request->user());
        GameEngine::answer($s, $request->user(), $request->input('payload', []));

        return GameEngine::view($s, $request->user());
    }

    public function pairs(Request $request)
    {
        $u = $request->user();
        $rows = PairRelationship::query()
            ->where(function ($q) use ($u) {
                $q->where('user_a', $u->id)->orWhere('user_b', $u->id);
            })
            ->get();
        $items = [];
        foreach ($rows as $p) {
            if (Safety::blockedEitherWay($p->user_a, $p->user_b)) {
                continue;
            }
            $your = $u->id === $p->user_a ? $p->a_action : $p->b_action;
            if ($p->state === 'closed' && $your !== 'pass') {
                continue;
            }
            $otherId = $u->id === $p->user_a ? $p->user_b : $p->user_a;
            $other = User::with('profile')->find($otherId);
            if (! $other) {
                continue;
            }
            $items[] = [
                'id' => $p->id,
                'state' => $p->state,
                'yourAction' => $your,
                'theyConnected' => ($u->id === $p->user_a ? $p->b_action : $p->a_action) === 'connect',
                'inviteEligible' => in_array($p->state, ['open_play', 'mutual', 'expired', 'unmatched'], true)
                    && (! $p->cooldown_until || $p->cooldown_until->isPast()),
                'other' => $this->card($other, $u),
            ];
        }

        return ['items' => $items];
    }

    public function pairShow(Request $request, string $id)
    {
        $u = $request->user();
        $p = PairRelationship::findOrFail($id);
        if ($p->user_a !== $u->id && $p->user_b !== $u->id) {
            abort(404);
        }
        Safety::assertNotBlocked($p->user_a, $p->user_b);
        $your = $u->id === $p->user_a ? $p->a_action : $p->b_action;
        if ($p->state === 'closed' && $your !== 'pass') {
            abort(404);
        }
        $otherId = $u->id === $p->user_a ? $p->user_b : $p->user_a;

        return [
            'id' => $p->id,
            'state' => $p->state,
            'yourAction' => $your,
            'theyConnected' => ($u->id === $p->user_a ? $p->b_action : $p->a_action) === 'connect',
            'other' => $this->card(User::with('profile')->findOrFail($otherId), $u),
        ];
    }

    public function pairAct(Request $request, string $id)
    {
        $u = $request->user();
        $action = $request->input('action');
        if (! in_array($action, ['connect', 'pass'], true)) {
            return response()->json(['error' => ['code' => 'bad', 'message' => 'connect or pass']], 422);
        }
        $p = PairRelationship::findOrFail($id);
        if ($p->user_a !== $u->id && $p->user_b !== $u->id) {
            abort(404);
        }
        Safety::assertNotBlocked($p->user_a, $p->user_b);
        if (! in_array($p->state, ['open_play', 'pending'], true)) {
            return response()->json(['error' => ['code' => 'closed', 'message' => 'That window closed']], 409);
        }
        if ($u->id === $p->user_a) {
            $p->a_action = $action;
        } else {
            $p->b_action = $action;
        }
        if ($p->a_action === 'pass' || $p->b_action === 'pass') {
            $p->state = 'closed';
            $p->cooldown_until = now()->addDays(14);
        } elseif ($p->a_action === 'connect' && $p->b_action === 'connect') {
            $this->mutual($p);
        } else {
            $p->state = 'pending';
            $p->pending_expires_at = now()->addHours(72);
        }
        $p->save();

        return $this->pairShow($request, $id);
    }

    private function mutual(PairRelationship $p): void
    {
        $p->state = 'mutual';
        $match = UserMatch::where('user_a', $p->user_a)->where('user_b', $p->user_b)->first();
        if (! $match) {
            $match = UserMatch::create([
                'id' => (string) Str::uuid(),
                'user_a' => $p->user_a,
                'user_b' => $p->user_b,
                'state' => 'mutual',
                'origin_session_id' => $p->origin_session_id,
                'matched_at' => now(),
                'expires_at' => now()->addDays(7),
            ]);
        } else {
            $match->state = 'mutual';
            $match->origin_session_id = $p->origin_session_id;
            $match->matched_at = now();
            $match->expires_at = now()->addDays(7);
            $match->unmatched_by = null;
            $match->save();
        }
        $thread = ChatThread::firstOrCreate(
            ['match_id' => $match->id],
            ['id' => (string) Str::uuid(), 'state' => 'open']
        );
        if ($thread->wasRecentlyCreated === false) {
            $thread->state = 'open';
            $thread->save();
            Message::create([
                'id' => (string) Str::uuid(),
                'thread_id' => $thread->id,
                'sender_id' => null,
                'kind' => 'system',
                'body' => 'You matched again',
            ]);
        }
        Message::create([
            'id' => (string) Str::uuid(),
            'thread_id' => $thread->id,
            'sender_id' => null,
            'kind' => 'icebreaker',
            'body' => 'You just played together — who picks the next move?',
            'meta' => ['actions' => ['me', 'them', 'compete', 'play_again']],
        ]);
        foreach ([$p->user_a, $p->user_b] as $uid) {
            XpEvent::create(['id' => (string) Str::uuid(), 'user_id' => $uid, 'kind' => 'mutual', 'amount' => 20]);
            User::where('id', $uid)->increment('xp', 20);
        }
    }

    public function threads(Request $request)
    {
        $u = $request->user();
        $matches = UserMatch::where('state', 'mutual')
            ->where(function ($q) use ($u) {
                $q->where('user_a', $u->id)->orWhere('user_b', $u->id);
            })->get();
        $items = [];
        foreach ($matches as $m) {
            if (Safety::blockedEitherWay($m->user_a, $m->user_b)) {
                continue;
            }
            $thread = ChatThread::where('match_id', $m->id)->first();
            $otherId = $u->id === $m->user_a ? $m->user_b : $m->user_a;
            $other = User::with('profile')->find($otherId);
            $last = $thread?->messages()->latest()->first();
            $items[] = [
                'threadId' => $thread?->id,
                'matchId' => $m->id,
                'state' => $thread?->state,
                'other' => $other ? $this->card($other, $u) : null,
                'last' => $last ? ['body' => $last->body, 'kind' => $last->kind, 'at' => $last->created_at] : null,
            ];
        }

        return ['items' => $items];
    }

    public function messages(Request $request, string $id)
    {
        $u = $request->user();
        $thread = ChatThread::findOrFail($id);
        $match = UserMatch::findOrFail($thread->match_id);
        if ($match->user_a !== $u->id && $match->user_b !== $u->id) {
            abort(404);
        }
        Safety::assertNotBlocked($match->user_a, $match->user_b);

        return [
            'state' => $thread->state,
            'items' => $thread->messages()->orderBy('created_at')->get()->map(fn ($m) => [
                'id' => $m->id,
                'senderId' => $m->sender_id,
                'kind' => $m->kind,
                'body' => $m->body,
                'meta' => $m->meta,
                'createdAt' => $m->created_at,
            ]),
        ];
    }

    public function sendMessage(Request $request, string $id)
    {
        $u = $request->user();
        $thread = ChatThread::findOrFail($id);
        $match = UserMatch::findOrFail($thread->match_id);
        if ($match->user_a !== $u->id && $match->user_b !== $u->id) {
            abort(404);
        }
        Safety::assertNotBlocked($match->user_a, $match->user_b);
        if ($thread->state !== 'open') {
            return response()->json(['error' => ['code' => 'locked', 'message' => 'Chat is locked']], 403);
        }
        $body = trim((string) $request->input('body'));
        if ($body === '') {
            return response()->json(['error' => ['code' => 'empty', 'message' => 'Type something']], 422);
        }
        $allowedIce = ["I'll pick", 'You pick', "Let's compete", 'Play again'];
        $kind = $request->input('kind', 'text');
        if ($kind === 'icebreaker' && ! in_array($body, $allowedIce, true)) {
            $kind = 'text';
        }
        $first = $thread->messages()->where('kind', 'text')->doesntExist();
        $msg = Message::create([
            'id' => (string) Str::uuid(),
            'thread_id' => $thread->id,
            'sender_id' => $u->id,
            'kind' => $kind === 'icebreaker' ? 'text' : 'text',
            'body' => $body,
        ]);
        if ($first) {
            XpEvent::create(['id' => (string) Str::uuid(), 'user_id' => $u->id, 'kind' => 'first_message', 'amount' => 10]);
            $u->increment('xp', 10);
        }
        $match->expires_at = null;
        $match->save();

        return ['id' => $msg->id];
    }

    public function inviteCreate(Request $request)
    {
        $u = $request->user();
        $target = $request->input('targetUserId');
        $kind = $request->input('gameKind', 'this_or_that');
        Safety::assertNotBlocked($u->id, $target);
        [$a, $b] = Safety::orderedPair($u->id, $target);
        $pair = PairRelationship::where('user_a', $a)->where('user_b', $b)->first();
        if (! $pair) {
            return response()->json(['error' => ['code' => 'unknown', 'message' => 'Play together first']], 409);
        }
        if ($pair->cooldown_until && $pair->cooldown_until->isFuture()) {
            return response()->json(['error' => ['code' => 'cooldown', 'message' => 'Try again later']], 409);
        }
        $inv = Invite::create([
            'id' => (string) Str::uuid(),
            'from_user_id' => $u->id,
            'to_user_id' => $target,
            'game_kind' => $kind,
            'state' => 'pending',
            'expires_at' => now()->addMinutes(2),
        ]);

        return $this->inviteItem($inv, $u);
    }

    public function inviteAccept(Request $request, string $id)
    {
        $u = $request->user();
        $inv = Invite::findOrFail($id);
        if ($inv->to_user_id !== $u->id || $inv->state !== 'pending') {
            abort(404);
        }
        $inv->state = 'accepted';
        $inv->save();
        $session = GameEngine::startSession($inv->game_kind, 'invite', [$inv->from_user_id, $inv->to_user_id]);

        return ['sessionId' => $session->id, 'gameKind' => $inv->game_kind];
    }

    public function block(Request $request)
    {
        $u = $request->user();
        $other = $request->input('userId');
        Block::firstOrCreate(
            ['blocker_id' => $u->id, 'blocked_id' => $other],
            ['id' => (string) Str::uuid()]
        );
        [$a, $b] = Safety::orderedPair($u->id, $other);
        $pair = PairRelationship::where('user_a', $a)->where('user_b', $b)->first();
        if ($pair) {
            $pair->state = 'blocked';
            $pair->save();
        }
        $match = UserMatch::where('user_a', $a)->where('user_b', $b)->first();
        if ($match) {
            $match->state = 'blocked';
            $match->save();
            ChatThread::where('match_id', $match->id)->update(['state' => 'locked']);
        }

        return ['ok' => true];
    }

    public function unblock(Request $request, string $id)
    {
        $u = $request->user();
        Block::where('blocker_id', $u->id)->where('blocked_id', $id)->delete();
        if (! Safety::blockedEitherWay($u->id, $id)) {
            [$a, $b] = Safety::orderedPair($u->id, $id);
            $pair = PairRelationship::where('user_a', $a)->where('user_b', $b)->first();
            if ($pair) {
                $pair->state = 'unmatched';
                $pair->cooldown_until = now()->addDays(14);
                $pair->save();
            }
            $match = UserMatch::where('user_a', $a)->where('user_b', $b)->first();
            if ($match) {
                $match->state = 'unmatched';
                $match->save();
            }
        }

        return ['ok' => true];
    }

    public function report(Request $request)
    {
        $u = $request->user();
        $reason = $request->input('reason', 'other');
        $subject = $request->input('userId');
        $rep = Report::create([
            'id' => (string) Str::uuid(),
            'reporter_id' => $u->id,
            'subject_id' => $subject,
            'reason' => $reason,
            'details' => $request->input('details'),
            'status' => 'open',
        ]);
        if (in_array($reason, ['csam', 'underage'], true) && $subject) {
            User::where('id', $subject)->update(['status' => 'hidden']);
            LegalHold::create([
                'id' => (string) Str::uuid(),
                'user_id' => $subject,
                'report_id' => $rep->id,
                'reason' => $reason,
                'status' => 'active',
                'purge_after' => now()->addDays(90),
            ]);
        }

        return ['ok' => true];
    }

    public function deleteMe(Request $request)
    {
        $u = $request->user();
        if (LegalHold::where('user_id', $u->id)->where('status', 'active')->exists()) {
            return response()->json(['error' => ['code' => 'legal_hold', 'message' => 'Your account cannot be deleted yet']], 409);
        }
        Photo::where('user_id', $u->id)->each(function ($p) {
            @unlink(storage_path('app/'.$p->path));
            @unlink(storage_path('app/'.$p->thumb_path));
        });
        $u->status = 'deleted';
        $u->name = 'Deleted';
        $u->email = null;
        $u->phone_e164_hash = $u->phone_e164_hash;
        $u->save();
        Auth::logout();

        return ['ok' => true];
    }

    public function legal()
    {
        return [
            'terms' => 'GameMatch is a play-first dating prototype. Matches are introductions, not safety guarantees. Be 18+. Be kind.',
            'privacy' => 'We store your profile, game answers, and messages to run matchmaking. Approximate city only — never exact GPS. You can delete your account in Me.',
        ];
    }

    public function inviteDecline(Request $request, string $id)
    {
        $u = $request->user();
        $inv = Invite::findOrFail($id);
        if ($inv->to_user_id !== $u->id) {
            abort(404);
        }
        $inv->state = 'declined';
        $inv->save();

        return ['ok' => true];
    }

    public function unmatch(Request $request, string $id)
    {
        $u = $request->user();
        $p = PairRelationship::findOrFail($id);
        if ($p->user_a !== $u->id && $p->user_b !== $u->id) {
            abort(404);
        }
        $p->state = 'unmatched';
        $p->cooldown_until = now()->addDays(14);
        $p->save();
        $match = UserMatch::where('user_a', $p->user_a)->where('user_b', $p->user_b)->first();
        if ($match) {
            $match->state = 'unmatched';
            $match->unmatched_by = $u->id;
            $match->save();
            ChatThread::where('match_id', $match->id)->update(['state' => 'locked']);
        }

        return ['ok' => true];
    }

    public function rematch(Request $request, string $id)
    {
        $u = $request->user();
        $s = GameSession::findOrFail($id);
        $ids = $s->participants()->pluck('user_id')->all();
        if (! in_array($u->id, $ids, true) || $s->mode === 'practice') {
            abort(404);
        }
        $other = collect($ids)->first(fn ($id) => $id !== $u->id);
        $request->merge(['targetUserId' => $other, 'gameKind' => $s->kind]);

        return $this->inviteCreate($request);
    }

    public function forcePair(Request $request)
    {
        $u = $request->user();
        if (! in_array($u->role, ['mod', 'admin'], true) || ! Flags::on('staff.force_pair')) {
            abort(404);
        }
        $other = User::findOrFail($request->input('otherUserId'));
        $kind = $request->input('gameKind', 'this_or_that');
        $session = GameEngine::startSession($kind, 'invite', [$u->id, $other->id]);

        return ['sessionId' => $session->id, 'gameKind' => $kind];
    }

    public function allowlist(Request $request)
    {
        if ($request->user()->role !== 'admin') {
            abort(403);
        }
        if ($request->isMethod('post')) {
            $phone = preg_replace('/\s+/', '', (string) $request->input('phone'));
            \Illuminate\Support\Facades\DB::table('allowlist_phones')->updateOrInsert(
                ['phone_e164_hash' => hash('sha256', $phone)],
                ['created_at' => now(), 'updated_at' => now()]
            );
        }
        if ($request->has('public_signup')) {
            FeatureFlag::updateOrCreate(
                ['key' => 'auth.public_signup'],
                ['value' => ['v' => (bool) $request->boolean('public_signup')]]
            );
        }

        return [
            'publicSignup' => Flags::on('auth.public_signup'),
            'count' => \Illuminate\Support\Facades\DB::table('allowlist_phones')->count(),
        ];
    }

    public function meXp(Request $request)
    {
        $u = $request->user();
        $events = XpEvent::where('user_id', $u->id)->latest()->limit(20)->get();
        $badges = [];
        if ($events->where('kind', 'session_complete')->count() >= 1) {
            $badges[] = ['id' => 'first-game', 'label' => 'First game'];
        }
        if ($events->where('kind', 'mutual')->count() >= 1) {
            $badges[] = ['id' => 'spark', 'label' => 'Spark'];
        }
        if ($events->where('kind', 'first_message')->count() >= 1) {
            $badges[] = ['id' => 'icebreaker', 'label' => 'Icebreaker'];
        }
        if ($u->xp >= 100) {
            $badges[] = ['id' => 'regular', 'label' => 'Regular'];
        }

        return ['xp' => $u->xp, 'level' => $u->level, 'badges' => $badges, 'events' => $events];
    }

    public function sessionLeave(Request $request, string $id)
    {
        $s = GameSession::findOrFail($id);
        if (! in_array($s->state, ['completed', 'forfeit', 'cancelled'], true)) {
            $s->state = 'forfeit';
            $s->ended_at = now();
            $s->save();
        }

        return ['ok' => true];
    }

    public function modReports(Request $request)
    {
        if (! in_array($request->user()->role, ['mod', 'admin'], true)) {
            abort(403);
        }

        return ['items' => Report::orderByDesc('created_at')->limit(100)->get()];
    }

    private function publicMe(User $u): array
    {
        $u->load(['profile', 'photos', 'intents', 'preferences']);

        return [
            'id' => $u->id,
            'name' => $u->name,
            'onboardingStep' => $u->onboarding_step,
            'status' => $u->status,
            'role' => $u->role,
            'xp' => $u->xp,
            'level' => $u->level,
            'incognito' => $u->incognito,
            'metroId' => $u->metro_id,
            'profile' => $u->profile,
            'photos' => $u->photos->map(fn ($p) => ['id' => $p->id, 'url' => url('/v1/photos/'.$p->id), 'state' => $p->moderation_state]),
            'intents' => $u->intents->pluck('intent'),
            'preferences' => $u->preferences,
        ];
    }

    private function card(User $u, User $viewer): array
    {
        $photo = $u->photos()->where('moderation_state', 'ok')->first();

        return [
            'id' => $u->id,
            'name' => $u->name,
            'age' => $u->profile?->age,
            'bio' => $u->profile?->bio,
            'photoUrl' => $photo ? url('/v1/photos/'.$photo->id) : null,
        ];
    }

    private function inviteItem(Invite $i, User $u): array
    {
        $otherId = $i->from_user_id === $u->id ? $i->to_user_id : $i->from_user_id;
        $other = User::with('profile')->find($otherId);

        return [
            'id' => $i->id,
            'gameKind' => $i->game_kind,
            'state' => $i->state,
            'expiresAt' => $i->expires_at,
            'fromMe' => $i->from_user_id === $u->id,
            'other' => $other ? $this->card($other, $u) : null,
        ];
    }
}
