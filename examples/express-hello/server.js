const express = require('express');
const os = require('os');

const app = express();
const PORT = process.env.PORT || 3000;

app.get('/', (req, res) => {
    res.json({
        message: 'Hello from Peer Compute! 🚀',
        hostname: os.hostname(),
        timestamp: new Date().toISOString(),
        env: {
            NODE_ENV: process.env.NODE_ENV || 'development',
            CUSTOM_MESSAGE: process.env.CUSTOM_MESSAGE || 'No custom message set'
        }
    });
});

app.get('/health', (req, res) => {
    res.json({ status: 'healthy' });
});

app.listen(PORT, '0.0.0.0', () => {
    console.log(`🚀 Express server running on port ${PORT}`);
});
