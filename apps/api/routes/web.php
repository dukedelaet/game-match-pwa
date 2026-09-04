<?php

use App\Http\Controllers\SpaController;
use Illuminate\Support\Facades\Route;

Route::fallback(SpaController::class);
