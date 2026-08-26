<?php

namespace Tests\Feature;

use App\Models\User;
use App\Support\Safety;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class PrototypeTest extends TestCase
{
    use RefreshDatabase;

    public function test_healthz(): void
    {
        $this->getJson('/v1/healthz')->assertOk()->assertJson(['ok' => true]);
    }

    public function test_otp_login_and_session(): void
    {
        $this->seed();
        $this->postJson('/v1/auth/otp/start', ['phone' => '+15551111111'])->assertOk();
        $this->postJson('/v1/auth/otp/verify', ['phone' => '+15551111111', 'code' => '123456'])
            ->assertOk()
            ->assertJsonPath('user.name', 'Alex');
        $this->getJson('/v1/auth/session')->assertOk()->assertJsonPath('user.name', 'Alex');
    }

    public function test_intents_ok_dating_pool(): void
    {
        $this->assertTrue(Safety::intentsOk(['dating', 'gaming'], ['dating']));
        $this->assertFalse(Safety::intentsOk(['dating'], ['friendship']));
        $this->assertFalse(Safety::intentsOk(['dating'], ['gaming']));
        $this->assertTrue(Safety::intentsOk(['friendship'], ['gaming']));
    }

    public function test_block_hides_pair_routes(): void
    {
        $this->seed();
        $alex = User::where('name', 'Alex')->first();
        $jordan = User::where('name', 'Jordan')->first();
        $this->actingAs($alex);
        $this->postJson('/v1/blocks', ['userId' => $jordan->id])->assertOk();
        $this->getJson('/v1/pairs')->assertOk()->assertJsonCount(0, 'items');
    }
}
