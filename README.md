# Truck Haul

A top-down 2D lorry driving game set in Britain. Haul cargo around **London**, **Cambridge** and **Manchester**, linked by the **M1**, **M11** and **M14** motorways and a central interchange. Deliver on time, obey the Highway Code, upgrade your rig (diesel or electric) and drive **online with your friends**.

**Download:** grab `TruckHaul.exe` from this repo and double-click it (Windows, nothing to install).
Windows SmartScreen may warn you the first time: click **More info → Run anyway**.

## How to play

1. Press **Enter** on the title screen (or **O** to play online).
2. Pick a job with **1**, **2** or **3**, or press **J** to make your own.
3. Follow the sat-nav (blue line) to the pick-up and stop in the **loading bay**.
4. Follow it again to the drop-off and stop in the bay to get paid.

## Controls

| Key | Action |
|---|---|
| W / ↑ | Accelerate |
| S / ↓ | Brake / reverse |
| A D / ← → | Steer |
| Space | Handbrake |
| Q / E | Indicate left / right (self-cancelling) |
| X | Hazard warning lights |
| H | Horn |
| 1 2 3 | Choose a job |
| J | Create your own job |
| M | Full map. **Right-click** to set a sat-nav destination |
| G | Clear the sat-nav pin |
| C | Cancel job |
| R | Call recovery when out of fuel / battery (£150) |
| T | Chat (online) |
| P / Esc | Pause |
| F | Fullscreen |

## Play online with friends (free)

Everyone connects to one small relay server. It just passes truck positions and chat between players. Money, jobs and saves stay on each person's PC.

**Set up the server once (free, about 10 minutes):**

1. Sign up at **https://render.com** using your GitHub account.
2. Let Render access the **truck-game** repo.
3. Click **New + → Web Service** and pick **truck-game**.
4. Settings:
   - **Branch:** `main`
   - **Root Directory:** `server`
   - **Runtime:** Go
   - **Build Command:** `go build -o server .`
   - **Start Command:** `./server`
   - **Instance Type:** Free
5. Click **Create Web Service** and wait until it says **Live**.
6. Copy the address at the top, for example `https://truck-haul-server-abcd.onrender.com`.

**Join:** everyone opens the game and presses **O** on the title screen. Paste the address with **Ctrl+V**, type a name and press **Enter**. Press **T** to chat.

The free server sleeps after about 15 minutes with nobody on. The first person to join afterwards waits up to a minute while it wakes up.

## Features

- **Three cities**: London, Cambridge (20 mph centre) and Manchester. Roads wind between junctions, which are proper square crossroads and T-junctions.
- **Motorways**: the M1 (London–Manchester), M11 (London–Cambridge) and M14 (Manchester–Cambridge). The M45 and M48 link the M1 and M11 to a central signal-controlled interchange. Motorways have 3 lanes and a hard shoulder each way, a central barrier, blue signs, and four service areas with slip roads, diesel lanes and EV charging bays.
- **Junctions**: traffic lights with a clear "your light" stop-line bar and dashboard display, plus **Stop** and **Give Way** junctions with signs and road markings.
- **Smart traffic**: cars, vans, buses and lorries drive on the left. They give way at junctions, and right-turners wait for oncoming traffic. They indicate, steer into corners, keep out of blocked junctions and hoot if you block them.
- **Incidents**: random collisions and roadworks close roads, and some roadworks use temporary traffic lights. Traffic news tells you where.
- **Emergency services**: sirens and blue lights. Traffic pulls over, and you must too.
- **Law and order**: speed limits, speed cameras and police pull-overs. Fines cost money only: there are no penalty points and no fines for red lights.
- **Sat-nav**: automatic routing to your job, or right-click anywhere on the map. It gives turn-by-turn instructions.
- **Jobs**: 40+ businesses with knock-off names (Teaco, Sainsbelly's, Amazin', Greggz, Kelloggz…). Cross-country jobs pay the most, and you can create your own jobs.
- **Fuel and charging**: PB, Crab, Essco, Texacoo, GridSwerve, Ionnity and the motorway services. EVs charge from 0–100% in 2 minutes.
- **Trucks**: Big Al's Truck Sales has 5 diesel and 2 electric trucks, and you can respray them in 11 colours.
- **Save files**: autosave to `%APPDATA%\TruckHaul\save.json`. Crashes are logged to `crash.log` in the same folder.

## Building from source

Written in Go with [Ebitengine](https://ebitengine.org/).

```bash
# Windows .exe (can be built from Linux/macOS too)
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H=windowsgui" -o TruckHaul.exe .
# Multiplayer server
cd server && go run .
```

## Changelog

- **v5**: online multiplayer (see friends' trucks live, name tags, map markers, chat) with a free relay server.
- **v4**: winding roads and fewer junctions; 3-lane motorways; M1 to Manchester; M14 and a central motorway interchange; Stop/Give Way junctions; much smarter traffic AI; sat-nav; custom jobs; horn; services slip roads and pump/EV lanes; no penalty points; cheaper fuel and better pay; big performance fixes.
- **v3**: M11 and Cambridge, electric trucks and charging, indicators and hazard lights, roadworks with temporary lights, sirens, save files.
- **v2**: bigger map, UK rules, traffic lights, smarter cars, incidents, emergency vehicles, police, speed cameras, fuel, truck dealer, paint shop.
- **v1**: first version.
