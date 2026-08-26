<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('metros', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->string('slug')->unique();
            $table->string('label');
            $table->decimal('centroid_lat', 8, 5)->nullable();
            $table->decimal('centroid_lng', 8, 5)->nullable();
            $table->json('adjacent_ids')->nullable();
            $table->timestamps();
        });

        Schema::create('genders', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->string('slug')->unique();
            $table->string('label');
            $table->unsignedInteger('sort')->default(0);
            $table->boolean('active')->default(true);
        });

        Schema::create('traits', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->string('slug')->unique();
            $table->string('label');
            $table->string('emoji', 8)->nullable();
            $table->unsignedInteger('sort')->default(0);
        });

        Schema::create('user_private', function (Blueprint $table) {
            $table->uuid('user_id')->primary();
            $table->string('phone_e164')->nullable();
            $table->timestamps();
        });

        Schema::create('profiles', function (Blueprint $table) {
            $table->uuid('user_id')->primary();
            $table->unsignedTinyInteger('age')->nullable();
            $table->string('city_label')->nullable();
            $table->string('bio', 280)->nullable();
            $table->uuid('gender_id')->nullable();
            $table->json('favorite_games')->nullable();
            $table->timestamps();
        });

        Schema::create('user_intents', function (Blueprint $table) {
            $table->uuid('user_id');
            $table->string('intent');
            $table->primary(['user_id', 'intent']);
        });

        Schema::create('user_traits', function (Blueprint $table) {
            $table->uuid('user_id');
            $table->uuid('trait_id');
            $table->primary(['user_id', 'trait_id']);
        });

        Schema::create('preferences', function (Blueprint $table) {
            $table->uuid('user_id')->primary();
            $table->unsignedTinyInteger('age_min')->default(18);
            $table->unsignedTinyInteger('age_max')->default(99);
            $table->string('distance_scope')->default('metro');
            $table->boolean('who_to_meet_open')->default(true);
            $table->json('who_to_meet')->nullable();
            $table->timestamps();
        });

        Schema::create('photos', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_id')->index();
            $table->string('path');
            $table->string('thumb_path')->nullable();
            $table->string('moderation_state')->default('ok');
            $table->string('blurhash')->nullable();
            $table->timestamps();
        });

        Schema::create('blocks', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('blocker_id')->index();
            $table->uuid('blocked_id')->index();
            $table->timestamps();
            $table->unique(['blocker_id', 'blocked_id']);
        });

        Schema::create('reports', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('reporter_id');
            $table->uuid('subject_id')->nullable();
            $table->string('reason');
            $table->text('details')->nullable();
            $table->string('status')->default('open');
            $table->timestamps();
        });

        Schema::create('legal_holds', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_id');
            $table->uuid('report_id')->nullable();
            $table->string('reason');
            $table->string('status')->default('active');
            $table->json('photo_keys')->nullable();
            $table->json('message_ids')->nullable();
            $table->timestamp('purge_after')->nullable();
            $table->timestamps();
        });

        Schema::create('feature_flags', function (Blueprint $table) {
            $table->string('key')->primary();
            $table->json('value');
        });

        Schema::create('allowlist_phones', function (Blueprint $table) {
            $table->string('phone_e164_hash')->primary();
            $table->timestamps();
        });

        Schema::create('prompt_bank', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->string('game_kind');
            $table->string('locale')->default('en');
            $table->json('payload');
            $table->json('tags')->nullable();
            $table->boolean('active')->default(true);
            $table->unsignedTinyInteger('nsfw_level')->default(0);
        });

        Schema::create('game_sessions', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->string('kind');
            $table->string('mode');
            $table->string('state')->default('pending');
            $table->timestamp('started_at')->nullable();
            $table->timestamp('ended_at')->nullable();
            $table->json('config')->nullable();
            $table->unsignedInteger('current_round')->default(0);
            $table->timestamp('answer_by')->nullable();
            $table->timestamp('forfeit_after')->nullable();
            $table->timestamps();
        });

        Schema::create('session_participants', function (Blueprint $table) {
            $table->uuid('session_id');
            $table->uuid('user_id');
            $table->unsignedTinyInteger('seat');
            $table->timestamp('joined_at')->nullable();
            $table->timestamp('last_poll_at')->nullable();
            $table->primary(['session_id', 'user_id']);
        });

        Schema::create('rounds', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('session_id')->index();
            $table->unsignedInteger('index');
            $table->uuid('prompt_id')->nullable();
            $table->string('state')->default('open');
            $table->timestamp('answer_by')->nullable();
            $table->json('extra')->nullable();
        });

        Schema::create('round_answers', function (Blueprint $table) {
            $table->uuid('round_id');
            $table->uuid('user_id');
            $table->json('payload');
            $table->timestamp('submitted_at')->nullable();
            $table->primary(['round_id', 'user_id']);
        });

        Schema::create('queue_entries', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_id')->unique();
            $table->uuid('metro_id');
            $table->string('game_kind');
            $table->timestamp('enqueued_at');
            $table->boolean('allow_practice')->default(false);
            $table->uuid('matched_session_id')->nullable();
        });

        Schema::create('pair_relationships', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_a');
            $table->uuid('user_b');
            $table->string('state')->default('open_play');
            $table->string('a_action')->default('none');
            $table->string('b_action')->default('none');
            $table->timestamp('pending_expires_at')->nullable();
            $table->timestamp('cooldown_until')->nullable();
            $table->uuid('origin_session_id')->nullable();
            $table->timestamps();
            $table->unique(['user_a', 'user_b']);
        });

        Schema::create('matches', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_a');
            $table->uuid('user_b');
            $table->string('state')->default('mutual');
            $table->uuid('origin_session_id')->nullable();
            $table->uuid('unmatched_by')->nullable();
            $table->timestamp('matched_at')->nullable();
            $table->timestamp('expires_at')->nullable();
            $table->timestamps();
            $table->unique(['user_a', 'user_b']);
        });

        Schema::create('chat_threads', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('match_id')->unique();
            $table->string('state')->default('open');
            $table->timestamps();
        });

        Schema::create('messages', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('thread_id')->index();
            $table->uuid('sender_id')->nullable();
            $table->string('kind')->default('text');
            $table->text('body');
            $table->json('meta')->nullable();
            $table->timestamps();
        });

        Schema::create('invites', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('from_user_id');
            $table->uuid('to_user_id');
            $table->string('game_kind');
            $table->string('state')->default('pending');
            $table->timestamp('expires_at');
            $table->timestamps();
        });

        Schema::create('signal_events', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_id')->index();
            $table->uuid('session_id')->nullable();
            $table->string('kind');
            $table->string('key');
            $table->json('value')->nullable();
            $table->timestamps();
        });

        Schema::create('compatibility_snapshots', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('session_id');
            $table->string('scorer_version')->default('scorer_v0');
            $table->uuid('user_a');
            $table->uuid('user_b');
            $table->float('score');
            $table->json('components')->nullable();
            $table->json('reasons')->nullable();
            $table->timestamp('computed_at');
            $table->unique(['session_id', 'scorer_version']);
        });

        Schema::create('xp_events', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->uuid('user_id')->index();
            $table->string('kind');
            $table->unsignedInteger('amount');
            $table->timestamps();
        });

        Schema::create('user_behavior_stats', function (Blueprint $table) {
            $table->uuid('user_id')->primary();
            $table->json('dims')->nullable();
            $table->timestamps();
        });
    }

    public function down(): void
    {
        $tables = [
            'user_behavior_stats', 'xp_events', 'compatibility_snapshots', 'signal_events',
            'invites', 'messages', 'chat_threads', 'matches', 'pair_relationships',
            'queue_entries', 'round_answers', 'rounds', 'session_participants', 'game_sessions',
            'prompt_bank', 'allowlist_phones', 'feature_flags', 'legal_holds', 'reports',
            'blocks', 'photos', 'preferences', 'user_traits', 'user_intents', 'profiles',
            'user_private', 'traits', 'genders', 'metros',
        ];
        foreach ($tables as $t) {
            Schema::dropIfExists($t);
        }
    }
};
