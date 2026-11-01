# Truck Haul

A top-down 2D lorry driving game set in a British town. Pick up cargo, deliver it on time, obey the Highway Code, and spend your earnings on better trucks.

**Download:** grab `TruckHaul.exe` from this repo and double-click it (Windows, nothing to install).
Windows SmartScreen may warn you the first time: click **More info → Run anyway**.

## How to play

1. Press **Enter** on the title screen.
2. Pick a job from the job board with **1**, **2** or **3**.
3. Follow the yellow arrow (and the minimap) to the pick-up yard, stop in the **loading bay**.
4. Follow the green arrow to the drop-off and stop in the bay to get paid.

Arrive late and your pay drops. Crash and your cargo is damaged.

## Controls

| Key | Action |
|---|---|
| W / ↑ | Accelerate |
| S / ↓ | Brake / reverse |
| A D / ← → | Steer |
| Space | Handbrake |
| 1 2 3 | Choose a job |
| M | Full city map |
| C | Cancel job |
| R | Call recovery when out of fuel (£300) |
| P / Esc | Pause |
| F | Fullscreen |

## Features

- **Big city**: 10 × 7 blocks with named streets (High Street, London Road, Station Road…) and 40 mph bypasses around the edge.
- **Job sites**: Hillside Sawmill, Meadow Farm, Rocky Top Quarry, Brewdawg Brewery, Amazin' Warehouse, Port of Bristow, Steelworks, Greggz Bakery, Royal Male Depot.
- **Customers**: Teaco Superstore, Sainsbelly's, Azda, Liddle, B&Queue DIY, Screwfixx, Argoz, McDougal's, Coasta Coffee, The Red Lion Pub and a building site.
- **Traffic lights** at every junction (red → red+amber → green → amber) with stop lines.
- **Smarter traffic**: cars, vans and buses that drive on the left, follow each other, stop at red lights, slow for corners and use their **indicators**.
- **Road incidents**: random road traffic collisions (police and ambulance on scene) and emergency roadworks close roads. Traffic news tells you where.
- **Emergency services**: ambulances, fire engines and police on blue lights. Traffic pulls over to the left, and you must too.
- **Law and order (UK-style)**:
  - Speed limit signs, plus a speed limit display on the dashboard
  - Yellow **speed cameras** (£100 + 3 points)
  - **Red-light cameras** at every junction (£100 + 3 points)
  - **Police patrols** pull you over for speeding, driving on the wrong side, driving on the pavement or careless driving. Stop when they flash you or face "failing to stop" (£1,000 + 6 points).
  - Failing to give way to an emergency vehicle (£100 + 3 points)
  - **12 penalty points = licence revoked = game over.** Points expire after 6 minutes.
- **Dashboard**: analogue speedometer (mph), fuel gauge, speed limit roundel, speed camera warning.
- **Fuel**: trucks burn diesel. Refuel at **PB**, **Crab Petrol**, **Essco** or **Texacoo** (prices differ).
- **Big Al's Truck Sales**: buy better trucks:

  | Truck | Price | Top speed |
  |---|---|---|
  | Bedfud TK (old banger) | starter | 42 mph |
  | Velvo PO 6 | £4,000 | 49 mph |
  | Sconia D | £9,000 | 55 mph |
  | PUF NGD | £16,000 | 61 mph |
  | Marcedes Actrus | £25,000 | 66 mph |

  Better trucks have faster acceleration, sharper handling, stronger brakes, better cargo protection, bigger tanks and better fuel economy.
- **Respray** any truck you own in 11 colours for £250.

## Building from source

Written in Go with [Ebitengine](https://ebitengine.org/).

```bash
# Windows .exe (can be built from Linux/macOS too)
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H=windowsgui" -o TruckHaul.exe .

# Run locally
go run .
```

## Changelog

- **v2**: bigger map, UK rules (drive on the left, mph, £), traffic lights, smarter cars with indicators, RTCs and roadworks, emergency vehicles, police pull-overs, speed cameras, speed limits, fuel and fuel stations, truck dealer with 5 trucks, paint shop.
- **v1**: first version with a small city, 8 job sites and simple traffic.
