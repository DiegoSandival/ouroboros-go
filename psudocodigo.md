# Ouroboros

base de datos cirular orientada a celulas de tamaño fijo

SWMR

celula

1. hash: [u8; 32]
2. salt: [u8; 16]
3. genoma: u32 (little-endian en disco)
4. x: u32
5. y: u32
6. z: u32


### psudo codigo

`append(celula)  ➝ celula_index`

`read(celula_index) ⤏ celula`

`read_auth(celula_index, secrete) ⤏ celula`

`update(celula_index, secrete, celula)⤏`

`update_auth(celula_index, secrete, celula)`