const express = require('express');
const cors = require('cors');
const path = require('path');

const app = express();
const PORT = 8080;
const TILE_DIR = path.join(__dirname, 'plateau_3dtiles');

const mimeTypes = {
  '.json': 'application/json',
  '.b3dm': 'application/octet-stream',
  '.pnts': 'application/octet-stream',
  '.i3dm': 'application/octet-stream',
  '.cmpt': 'application/octet-stream',
  '.glb': 'model/gltf-binary',
  '.gltf': 'model/gltf+json'
};

app.use(cors({
  origin: '*',
  methods: ['GET', 'HEAD'],
  allowedHeaders: ['Content-Type', 'Authorization']
}));

app.use(express.static(TILE_DIR, {
  setHeaders: (res, filePath) => {
    const ext = path.extname(filePath).toLowerCase();
    if (mimeTypes[ext]) {
      res.setHeader('Content-Type', mimeTypes[ext]);
    }
    res.setHeader('Cache-Control', 'public, max-age=86400');
  }
}));

app.get('/health', (req, res) => {
  res.json({ status: 'ok', message: 'PLATEAU 3D Tiles server running' });
});

app.listen(PORT, () => {
  console.log('CORS-enabled 3D Tiles server running at http://localhost:' + PORT);
  console.log('Serving tiles from: ' + TILE_DIR);
  console.log('Test tileset URL: http://localhost:' + PORT + '/tileset.json');
});
