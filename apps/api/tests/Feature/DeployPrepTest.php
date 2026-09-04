<?php

namespace Tests\Feature;

use App\Support\Flags;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Artisan;
use Tests\TestCase;

class DeployPrepTest extends TestCase
{
    use RefreshDatabase;

    public function test_healthz_hits_the_database(): void
    {
        $this->getJson('/v1/healthz')->assertOk()->assertJson(['ok' => true]);
    }

    public function test_catalog_seeder_keeps_public_signup_off(): void
    {
        $this->seed(\Database\Seeders\CatalogSeeder::class);
        $this->assertFalse(Flags::on('auth.public_signup'));
        $this->assertFalse(Flags::on('staff.force_pair'));
        $this->assertTrue(Flags::on('games.enabled'));
        $this->assertDatabaseHas('users', ['id' => config('gamematch.house_user_id'), 'name' => 'House']);
        $this->seed(\Database\Seeders\CatalogSeeder::class);
        $this->assertSame(1, \App\Models\Metro::where('slug', 'los-angeles')->count());
    }

    public function test_full_seed_still_has_demo_users(): void
    {
        $this->seed();
        $this->assertTrue(Flags::on('auth.public_signup'));
        $this->assertNotNull(\App\Models\User::where('name', 'Alex')->first());
    }

    public function test_spa_fallback_skips_v1_and_up(): void
    {
        $index = public_path('index.html');
        file_put_contents($index, '<!doctype html><html><body><div id="root">GameMatch</div></body></html>');
        try {
            $this->get('/boot')->assertOk()->assertSee('GameMatch', false);
            $this->getJson('/v1/healthz')->assertOk()->assertJson(['ok' => true]);
            $this->get('/up')->assertOk();
            $this->assertSame(0, Artisan::call('route:cache'));
        } finally {
            Artisan::call('route:clear');
            @unlink($index);
        }
    }

    public function test_otp_artisan_prints_cached_code(): void
    {
        $this->seed(\Database\Seeders\CatalogSeeder::class);
        \Illuminate\Support\Facades\Cache::put('otp:+15559990000', '654321', 600);
        $this->artisan('gamematch:otp', ['phone' => '+15559990000'])
            ->expectsOutput('654321')
            ->assertSuccessful();
        $this->artisan('gamematch:make-admin', ['phone' => '+15559990000'])->assertSuccessful();
        $this->assertDatabaseHas('users', [
            'phone_e164_hash' => hash('sha256', '+15559990000'),
            'role' => 'admin',
        ]);
    }
}
