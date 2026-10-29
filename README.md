# WiFiDrop

Transferencia rápida de archivos por Wi‑Fi entre el móvil y el ordenador, y
entre ordenadores. Sin internet, sin cuentas, sin límite de tamaño: los
archivos viajan directamente por la red local a la velocidad del Wi‑Fi.

- **Escritorio**: un único ejecutable para Windows, macOS y Linux. Al abrirlo
  muestra la interfaz en el navegador, con un código QR para conectar el móvil.
- **Android**: app nativa (`wifidrop-android.apk`) que encuentra sola el
  ordenador en la red, abre su interfaz y permite enviar archivos desde el
  menú «Compartir» de cualquier app (fotos, vídeos, documentos…).
- **iPhone / iPad**: se usa la misma interfaz desde Safari; con «Añadir a
  pantalla de inicio» queda instalada como app.
- **Seguridad**: cada equipo tiene un PIN de 6 dígitos. Solo quien lo conoce
  (o escanea el QR) puede enviar o descargar archivos. Tras cinco intentos
  fallidos se bloquea la dirección 15 segundos.

## Cómo funciona

1. Ejecuta `wifidrop` en el ordenador. Se abre `http://localhost:8750/` y
   aparece el PIN, la dirección en la red y el QR.
2. Escanea el QR con el móvil (en la misma Wi‑Fi) o abre la app Android, que
   lista los ordenadores encontrados.
3. Envía archivos tocando «Elegir archivos» o arrastrándolos. Los recibidos
   aparecen en la lista y se guardan en `Descargas/WiFiDrop`.
4. Para enviar del ordenador al móvil, basta con que el archivo esté en la
   carpeta: desde el móvil se descarga con «Guardar».
5. Entre ordenadores: ambos ejecutan `wifidrop`, se ven en «Equipos en la
   red» y se envían archivos con el PIN del destino (se pide una sola vez).

## Instalación

### Descargas

En la página de **Releases** del repositorio hay siempre una entrada
«Última versión (main)» con los archivos compilados automáticamente:

| Dispositivo | Archivo |
|-------------|---------|
| Android | `wifidrop-android.apk` |
| Windows | `wifidrop-windows-amd64.exe` |
| macOS Apple Silicon | `wifidrop-darwin-arm64` |
| macOS Intel | `wifidrop-darwin-amd64` |
| Linux | `wifidrop-linux-amd64` |

### Escritorio

Descarga el binario de tu sistema y ábrelo. La primera vez se instala solo:
se copia a una carpeta estable y crea el acceso directo con su icono
(Escritorio y menú Inicio en Windows, `WiFiDrop.app` en `~/Applications` en
macOS, lanzador en el menú de aplicaciones en Linux). A partir de ahí se abre
como cualquier otra app y se cierra con el botón «Salir» de la interfaz.

- **Windows**: SmartScreen puede avisar porque el ejecutable no está firmado;
  pulsa «Más información → Ejecutar de todas formas».
- **macOS**: si Gatekeeper lo bloquea, haz clic derecho → «Abrir» la primera
  vez. En la terminal: `chmod +x wifidrop-darwin-arm64 && ./wifidrop-darwin-arm64`.
- **Linux**: `chmod +x wifidrop-linux-amd64 && ./wifidrop-linux-amd64`.

`wifidrop install` y `wifidrop uninstall` crean o quitan el acceso directo a
mano; `-no-install` evita que se cree en el primer arranque.

Opciones útiles:

```
wifidrop -dir ~/Compartido     # carpeta de recepción
wifidrop -name "Portátil"      # nombre visible para los demás
wifidrop -pin 123456           # fijar el PIN (se guarda)
wifidrop -open                 # aceptar de cualquiera de la red sin PIN
wifidrop -port 9000            # otro puerto (por defecto 8750)
wifidrop -no-browser           # no abrir el navegador
wifidrop send -pin 222222 192.168.1.20 foto.jpg video.mp4   # enviar desde la terminal
```

El PIN y el nombre se guardan en la carpeta de configuración del usuario
(`~/.config/wifidrop/config.json` en Linux, `%AppData%\wifidrop` en Windows,
`~/Library/Application Support/wifidrop` en macOS).

Si el cortafuegos pregunta, permite el acceso en redes privadas: el programa
escucha en el puerto TCP 8750 y en el UDP 8751 (descubrimiento).

### Android

Instala `wifidrop-android.apk` desde *Releases* (hay que permitir «instalar
apps desconocidas» la primera vez). La app:

- Muestra los ordenadores con WiFiDrop en la red (o se añaden por IP).
- Al tocar uno abre su interfaz; la primera vez pide el PIN.
- Aparece en el menú «Compartir» del sistema para enviar archivos directamente.

### iPhone

Escanea el QR del ordenador con la cámara, y en Safari usa
«Compartir → Añadir a pantalla de inicio».

## Compilar

Requisitos: Go 1.24 o superior.

```
go build .                 # binario para este sistema
./build.sh                 # todos los sistemas, en ./dist
go test ./...              # pruebas
go run ./tools/genicons    # regenerar los iconos PNG a partir del diseño
go run ./tools/winres      # recursos del .exe de Windows (icono, versión); build.sh ya lo hace
```

La app Android se compila con Gradle 8.9, JDK 17 y el SDK de Android:

```
gradle -p android assembleDebug
```

GitHub Actions compila todo en cada push y, al crear una etiqueta `vX.Y.Z`,
publica los binarios y el APK en una *Release*.

## Arquitectura

```
┌───────────── Ordenador ─────────────┐      ┌──────── Móvil ────────┐
│ wifidrop (Go)                        │      │ App Android / Safari  │
│  ├ HTTP :8750  interfaz + API        │◄────►│  WebView o navegador  │
│  ├ UDP  :8751  anuncio cada 2 s      │─────►│  escucha anuncios     │
│  └ carpeta Descargas/WiFiDrop        │      │  Compartir → /api/upload
└──────────────────────────────────────┘      └───────────────────────┘
```

- `main.go` arranque, opciones, subcomando `send`.
- `server.go` API HTTP, autenticación por PIN, eventos en vivo (SSE), QR.
- `discovery.go` descubrimiento por difusión UDP.
- `storage.go` guardado seguro de archivos (nombres saneados, sin sobrescribir).
- `web/` interfaz (PWA): HTML, CSS y JavaScript sin dependencias.
- `android/` app Android en Kotlin.

### API

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/info` | Nombre, versión; incluye el PIN solo desde el propio equipo |
| POST | `/api/auth` | Comprueba un PIN (`{"pin":"123456"}`) |
| GET | `/api/peers` | Equipos descubiertos en la red |
| GET | `/api/files` | Archivos de la carpeta |
| GET | `/api/files/{nombre}` | Descarga |
| DELETE | `/api/files/{nombre}` | Elimina |
| POST | `/api/upload` | Sube archivos (`multipart/form-data`, campo `file`) |
| POST | `/api/send?peer=ID&pin=…` | Reenvía la subida a otro equipo |
| GET | `/api/events` | Eventos en vivo (archivo recibido, eliminado) |
| GET | `/api/qr` | QR en PNG con la dirección (y el PIN desde el propio equipo) |

El PIN se envía en la cabecera `X-WifiDrop-Key` o en el parámetro `key`.
