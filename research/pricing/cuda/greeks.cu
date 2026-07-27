// Phase 3: Intraday Risk & Greeks Calculation (CUDA)
// Computes Black-Scholes Delta, Gamma, Vega, Theta across a GPU block

#include <math.h>
#include <stdio.h>

// Standard Normal CDF approximation for device
__device__ float norm_cdf(float x) {
    return 0.5f * (1.0f + erff(x / sqrtf(2.0f)));
}

__device__ float norm_pdf(float x) {
    const float inv_sqrt_2pi = 0.3989422804014327f;
    return inv_sqrt_2pi * expf(-0.5f * x * x);
}

// CUDA Kernel to calculate Call Delta and Gamma for an array of options
__global__ void calculate_greeks_kernel(
    const float* S, // Spot Price
    const float* K, // Strike
    const float* T, // Time to maturity
    const float* r, // Risk-free rate
    const float* v, // Volatility
    float* delta, 
    float* gamma, 
    int num_options
) {
    int idx = blockIdx.x * blockDim.x + threadIdx.x;
    
    if (idx < num_options) {
        float spot = S[idx];
        float strike = K[idx];
        float time = T[idx];
        float rate = r[idx];
        float vol = v[idx];

        float d1 = (logf(spot / strike) + (rate + 0.5f * vol * vol) * time) / (vol * sqrtf(time));
        
        // Delta (Call)
        delta[idx] = norm_cdf(d1);
        
        // Gamma
        gamma[idx] = norm_pdf(d1) / (spot * vol * sqrtf(time));
    }
}
