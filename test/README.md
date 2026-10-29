# Prueba de interfaz (Playwright)

Arranca dos instancias en esta misma máquina y ejecuta la prueba:

```bash
go build -o /tmp/wifidrop .
head -c 2000000 /dev/urandom > test/fixtures/big.bin
XDG_CONFIG_HOME=/tmp/cfg1 /tmp/wifidrop -no-browser -port 8750 -dir /tmp/d1 -name PC-Uno -pin 111111 &
XDG_CONFIG_HOME=/tmp/cfg2 /tmp/wifidrop -no-browser -port 8760 -dir /tmp/d2 -name PC-Dos -pin 222222 &
node test/ui_test.js
```

La prueba usa `192.0.2.2` como "dirección de red no local"; cámbiala por una IP
de tu máquina si es distinta. Requiere `playwright` instalado con Chromium.
