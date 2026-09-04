<?php

namespace App\Http\Controllers;

class SpaController extends Controller
{
    public function __invoke()
    {
        $index = public_path('index.html');
        abort_unless(is_file($index), 500, 'SPA index.html missing');

        return response(file_get_contents($index), 200, [
            'Content-Type' => 'text/html; charset=UTF-8',
        ]);
    }
}
