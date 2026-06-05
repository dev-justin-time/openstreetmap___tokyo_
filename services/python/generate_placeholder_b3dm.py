"""Generate a placeholder .b3dm with a simple box building for 3D Tiles preview."""

import struct
import json
import os

OUT_DIR = os.path.join(os.path.dirname(__file__), '..', 'plateau-server', 'plateau_3dtiles')

# ─── Box geometry (unit box centered at origin, scaled later in tileset) ─
verts = [
    -1, -1, -1,   1, -1, -1,   1,  1, -1,  -1,  1, -1,
    -1, -1,  1,   1, -1,  1,   1,  1,  1,  -1,  1,  1,
]

pos_data = bytearray()
nrm_data = bytearray()
idx_data = bytearray()
vertex_offset = 0
for vi, n in [
    ([0,1,2,3], (0,0,-1)),  # back
    ([4,5,6,7], (0,0,1)),   # front
    ([1,5,6,2], (1,0,0)),   # right
    ([0,4,7,3], (-1,0,0)),  # left
    ([3,7,6,2], (0,1,0)),   # top
    ([0,1,5,4], (0,-1,0)),  # bottom
]:
    for v_idx in vi:
        i = v_idx * 3
        pos_data.extend(struct.pack('fff', verts[i], verts[i+1], verts[i+2]))
        nrm_data.extend(struct.pack('fff', *n))
    idx_data.extend(struct.pack('HHH', vertex_offset, vertex_offset+1, vertex_offset+2))
    idx_data.extend(struct.pack('HHH', vertex_offset, vertex_offset+2, vertex_offset+3))
    vertex_offset += 4

# ─── Build GLB ───────────────────────────────────────────────────────────
gltf = {
    "asset": {"version": "2.0", "generator": "kinetic-fleet-placeholder"},
    "scene": 0,
    "scenes": [{"nodes": [0]}],
    "nodes": [{"mesh": 0}],
    "meshes": [{"primitives": [{"attributes": {"POSITION": 0, "NORMAL": 1}, "indices": 2, "material": 0}]}],
    "materials": [{
        "pbrMetallicRoughness": {"baseColorFactor": [0.15, 0.35, 0.75, 1.0], "metallicFactor": 0.3, "roughnessFactor": 0.6}
    }],
    "accessors": [
        {"componentType": 5126, "count": 24, "type": "VEC3", "max": [1,1,1], "min": [-1,-1,-1], "bufferView": 0, "byteOffset": 0},
        {"componentType": 5126, "count": 24, "type": "VEC3", "bufferView": 1, "byteOffset": 0},
        {"componentType": 5123, "count": 36, "type": "SCALAR", "bufferView": 2, "byteOffset": 0},
    ],
    "bufferViews": [
        {"buffer": 0, "byteOffset": 0, "byteLength": len(pos_data), "target": 34962},
        {"buffer": 0, "byteOffset": len(pos_data), "byteLength": len(nrm_data), "target": 34962},
        {"buffer": 0, "byteOffset": len(pos_data)+len(nrm_data), "byteLength": len(idx_data), "target": 34963},
    ],
    "buffers": [{"byteLength": len(pos_data)+len(nrm_data)+len(idx_data)}],
}
json_str = json.dumps(gltf, separators=(',', ':'))
json_pad = (4 - (len(json_str) % 4)) % 4
json_str += ' ' * json_pad

bin_data = bytes(pos_data + nrm_data + idx_data)
bin_pad = (4 - (len(bin_data) % 4)) % 4
bin_data += b'\x00' * bin_pad

glb_len = 12 + 8 + len(json_str) + 8 + len(bin_data)
glb = bytearray()
glb.extend(b'glTF')
glb.extend(struct.pack('<II', 2, glb_len))
glb.extend(struct.pack('<II', len(json_str), 0x4E4F534A))
glb.extend(json_str.encode())
glb.extend(struct.pack('<II', len(bin_data), 0x004E4942))
glb.extend(bin_data)

# ─── Build B3DM ──────────────────────────────────────────────────────────
B3DM_HEADER_LEN = 28
# Feature table: MUST contain BATCH_LENGTH for Cesium
ft_json = b'{"BATCH_LENGTH":1}'
ft_bin = b''
batch_json = b'{}'
batch_bin = b''

def pad4(data):
    p = (4 - (len(data) % 4)) % 4
    return data + b'\x00' * p

ft_json_p = pad4(ft_json)
ft_bin_p = pad4(ft_bin)
batch_json_p = pad4(batch_json)
batch_bin_p = pad4(batch_bin)

# Align glb start to 8-byte boundary from b3dm header start
tables_len = len(ft_json_p) + len(ft_bin_p) + len(batch_json_p) + len(batch_bin_p)
glb_offset = B3DM_HEADER_LEN + tables_len
# Calculate padding to align glb to 8 bytes
glb_align = (8 - (glb_offset % 8)) % 8
glb_offset += glb_align  # not directly used since we pad the tables section

total_len = B3DM_HEADER_LEN + len(ft_json_p) + len(ft_bin_p) + len(batch_json_p) + len(batch_bin_p) + glb_align + len(glb)

b3dm = bytearray()
b3dm.extend(b'b3dm')
b3dm.extend(struct.pack('<IIIIII',
    1, total_len,
    len(ft_json_p), len(ft_bin_p),
    len(batch_json_p), len(batch_bin_p),
))
b3dm.extend(ft_json_p)
b3dm.extend(ft_bin_p)
b3dm.extend(batch_json_p)
b3dm.extend(batch_bin_p)
b3dm.extend(b'\x00' * glb_align)
b3dm.extend(glb)

# ─── Write files ─────────────────────────────────────────────────────────
os.makedirs(OUT_DIR, exist_ok=True)
b3dm_path = os.path.join(OUT_DIR, 'buildings.b3dm')
with open(b3dm_path, 'wb') as f:
    f.write(b3dm)

# Scale the box to a ~100m building centered on Tokyo Station
SCALE = 50  # half-width in meters (total ~100m building)
tileset = {
    "asset": {"version": "1.0", "generator": "kinetic-fleet-placeholder"},
    "geometricError": 500,
    "root": {
        "boundingVolume": {"region": [139.767, 35.681, 139.768, 35.682, 0, 150]},
        "refine": "ADD",
        "geometricError": 500,
        "transform": [SCALE,0,0,0, 0,SCALE,0,0, 0,0,SCALE,0, 139.7671,35.6812,0,1],
        "content": {"uri": "buildings.b3dm"},
        "children": []
    }
}
tileset_path = os.path.join(OUT_DIR, 'tileset.json')
with open(tileset_path, 'w') as f:
    json.dump(tileset, f, indent=2)
print(f"Wrote {b3dm_path} ({len(b3dm)} bytes)")
print(f"Wrote {tileset_path}")
print("Done. Restart nyc-app-server and reload.")

