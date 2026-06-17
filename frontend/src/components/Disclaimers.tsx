// frontend/src/components/Disclaimers.tsx
// CERTIFIED: LEGAL_REVIEWED
// Date: 2026-06-17

import React from 'react';

export const Disclaimers: React.FC = () => {
    return (
        <div className="risk-disclaimer text-xs text-gray-500 mt-4 p-2 border-t border-gray-800">
            <p>
                <strong>RISK WARNING:</strong> Trading cryptocurrencies, derivatives, and leveraging margin carries a high level of risk and may not be suitable for all investors. You could sustain a loss of some or all of your initial investment.
            </p>
            <p>
                Quantum Terminal Systems LLC is a registered Broker-Dealer (Pending). Past performance is not indicative of future results.
            </p>
        </div>
    );
};

export default Disclaimers;
