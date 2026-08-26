<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('users', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->string('name')->nullable();
            $table->string('email')->nullable()->unique();
            $table->string('phone_e164_hash')->nullable()->unique();
            $table->string('role')->default('user');
            $table->string('onboarding_step')->default('welcome');
            $table->string('status')->default('pending');
            $table->timestamp('age_attested_at')->nullable();
            $table->date('dob')->nullable();
            $table->boolean('incognito')->default(false);
            $table->boolean('hidden')->default(false);
            $table->timestamp('last_seen_at')->nullable();
            $table->date('last_active_on')->nullable();
            $table->uuid('metro_id')->nullable()->index();
            $table->string('approx_geohash', 8)->nullable();
            $table->unsignedInteger('xp')->default(0);
            $table->unsignedInteger('level')->default(1);
            $table->rememberToken();
            $table->timestamps();
        });

        Schema::create('password_reset_tokens', function (Blueprint $table) {
            $table->string('email')->primary();
            $table->string('token');
            $table->timestamp('created_at')->nullable();
        });

        Schema::create('sessions', function (Blueprint $table) {
            $table->string('id')->primary();
            $table->uuid('user_id')->nullable()->index();
            $table->string('ip_address', 45)->nullable();
            $table->text('user_agent')->nullable();
            $table->longText('payload');
            $table->integer('last_activity')->index();
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('users');
        Schema::dropIfExists('password_reset_tokens');
        Schema::dropIfExists('sessions');
    }
};
