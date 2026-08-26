<?php

use App\Http\Controllers\ApiController;
use Illuminate\Support\Facades\Route;

Route::get('/healthz', [ApiController::class, 'health']);
Route::get('/catalogs', [ApiController::class, 'catalogs']);
Route::get('/legal', [ApiController::class, 'legal']);
Route::get('/games', [ApiController::class, 'games']);

Route::post('/auth/otp/start', [ApiController::class, 'otpStart']);
Route::post('/auth/otp/verify', [ApiController::class, 'otpVerify']);
Route::post('/auth/oauth/{provider}', [ApiController::class, 'oauthDemo']);
Route::post('/auth/logout', [ApiController::class, 'logout']);
Route::get('/auth/session', [ApiController::class, 'sessionUser']);

Route::middleware('auth')->group(function () {
    Route::get('/me', [ApiController::class, 'sessionUser']);
    Route::patch('/me', [ApiController::class, 'patchMe']);
    Route::post('/me/photos', [ApiController::class, 'uploadPhoto']);
    Route::delete('/me', [ApiController::class, 'deleteMe']);
    Route::get('/photos/{id}', [ApiController::class, 'photo']);
    Route::get('/home', [ApiController::class, 'home']);
    Route::post('/queue', [ApiController::class, 'queueJoin']);
    Route::get('/queue/status', [ApiController::class, 'queueStatus']);
    Route::delete('/queue', [ApiController::class, 'queueLeave']);
    Route::post('/sessions/{id}/join', [ApiController::class, 'sessionJoin']);
    Route::get('/sessions/{id}', [ApiController::class, 'sessionShow']);
    Route::post('/sessions/{id}/answer', [ApiController::class, 'sessionAnswer']);
    Route::post('/sessions/{id}/leave', [ApiController::class, 'sessionLeave']);
    Route::post('/sessions/{id}/rematch', [ApiController::class, 'rematch']);
    Route::post('/pairs/{id}/unmatch', [ApiController::class, 'unmatch']);
    Route::get('/me/xp', [ApiController::class, 'meXp']);
    Route::match(['get', 'post'], '/staff/allowlist', [ApiController::class, 'allowlist']);
    Route::post('/internal/force-pair', [ApiController::class, 'forcePair']);
    Route::get('/pairs', [ApiController::class, 'pairs']);
    Route::get('/pairs/{id}', [ApiController::class, 'pairShow']);
    Route::post('/pairs/{id}/connect', [ApiController::class, 'pairAct']);
    Route::get('/threads', [ApiController::class, 'threads']);
    Route::get('/threads/{id}/messages', [ApiController::class, 'messages']);
    Route::post('/threads/{id}/messages', [ApiController::class, 'sendMessage']);
    Route::post('/invites', [ApiController::class, 'inviteCreate']);
    Route::post('/invites/{id}/accept', [ApiController::class, 'inviteAccept']);
    Route::post('/invites/{id}/decline', [ApiController::class, 'inviteDecline']);
    Route::post('/blocks', [ApiController::class, 'block']);
    Route::delete('/blocks/{id}', [ApiController::class, 'unblock']);
    Route::post('/reports', [ApiController::class, 'report']);
    Route::get('/internal/mod/reports', [ApiController::class, 'modReports']);
});
