# Serveur de jeu multijoueur

Ce projet est un serveur de jeu temps réel écrit en **Go**. Il gère la logique d'un jeu de type "Pac-Man multijoueur" où les clients se connectent via **WebSockets** pour se déplacer et collecter des bonbons sur une grille partagée.
Il a été réalisé pour fonctionner avec le client: https://github.com/dntelisa/SR-S9-Projet-Client

## Fonctionnalités

* **Communication temps réel :** Utilisation de WebSockets (via `gorilla/websocket`) pour une communication bidirectionnelle rapide.
* **Architecture concurrente :** Utilisation des **Goroutines** et des **Channels** pour gérer de multiples connexions simultanées sans bloquer le serveur.
* **Sécurité des données (thread-safety) :** Utilisation de `sync.Mutex` pour protéger l'état du jeu (positions des joueurs, liste des bonbons) contre les accès concurrents (Race Conditions).
* **Game loop déterministe :** Une boucle de jeu tourne à une fréquence fixe (20 Ticks/seconde) pour mettre à jour la physique et diffuser l'état du monde (`snapshot`) aux clients.
* **Gestion des collisions :** Calcul côté serveur des déplacements et des interactions (entre joueurs, avec les bonbons).
* **Déploiement flexible :** Port d'écoute configurable avec des arguments en ligne de commande.

## Prérequis

* **Go** (version 1.20 ou supérieure recommandée)

## Installation

1.  Cloner le dépôt :
    ```bash
    git clone [https://github.com/dntelisa/SR-S9-Projet-Serveur.git](https://github.com/dntelisa/SR-S9-Projet-Serveur.git)
    cd SR-S9-Projet-Serveur
    ```

2.  Télécharger les dépendances :
    ```bash
    go mod download
    ```

## Démarrage

### Lancement standard (Localhost)
Par défaut, le serveur écoute sur le port **8080**.
```bash
go run .

```

### Lancement sur un port spécifique

Pour changer l'adresse ou le port (ex: pour écouter sur le port 80 ou sur toutes les interfaces réseau dans une VM) :

```bash
# Écouter sur le port 80 (avec sudo pour Linux)
sudo go run . -addr :80

```

Une fois lancé, le serveur est accessible via : `ws://localhost:8080/ws` ou avec l'IP de la VM: `ws://<IP-VM>:port/ws` .

## Architecture

Le projet est structuré de cette manière:

### 1. Point d'Entrée (`superserveur.go`)

* Gère les arguments de ligne de commande (`flag.Parse()`).
* Initialise le serveur HTTP et les routes.

### 2. Gestion Réseau (`server/routes.go` & `server/ws.go`)

* **Hub WebSocket :** Maintient la liste des clients connectés.
* **Pattern Reader/Writer :** Chaque client possède deux Goroutines (`readPump` et `writePump`) pour lire les entrées et envoyer les mises à jour de manière asynchrone.
* **Broadcast :** Diffuse l'état du jeu calculé par le moteur à tous les clients connectés.

### 3. Moteur de Jeu (`server/game/game.go`)

Il gère toute la logique de l'application

* **Structures :** Définit `Player`, `Sweet`, et `Game`.
* **Boucle Principale (`Start`) :** Exécutée via un `time.Ticker`, elle orchestre le jeu :
1. Applique les commandes des joueurs (validations, collisions).
2. Vérifie les conditions de victoire (plus de bonbons).
3. Génère un snapshot de l'état (`broadcastState`).


* **Mutex (`sync.Mutex`) :** Verrouille l'accès aux cartes `players` et `sweets` lors des modifications pour garantir l'intégrité de la mémoire.

##  Protocole de communication (JSON)

Le serveur et le client échangent des messages au format JSON.

**Exemple : Connexion (Client -> Serveur)**

```json
{
  "type": "join",
  "name": "Elisa"
}

```

**Exemple : Déplacement (Client -> Serveur)**

```json
{
  "type": "move",
  "dir": "up"
}

```

**Exemple : État du Monde (Serveur -> Client)**
Envoyé 20 fois par seconde (Broadcast).

```json
{
  "type": "state",
  "tick": 450,
  "players": [
    {"id": "p-1", "x": 10, "y": 5, "score": 12}
  ],
  "sweets": [
    {"id": "s1", "x": 2, "y": 3}
  ]
}

```

## Tests

Des fonctions utilitaires sont exposées dans `game.go` (`SetSweet`, `ClearSweets`) pour faciliter les tests d'intégration et les tests unitaires.
Plusieurs tests ont été réalisé:
* **End to end**: routes/e2e_test.go
* **Test d'intégration**: routes/integration_test.go pour tester des scénarios specifiques comme l'arrivée de deux joueurs au même moment sur un bonbon
* **Tests unitaires**: game/game_test.go et game/event_test.go pour tester les fonctionnalités du serveur comme par exemple l'ajout d'un joueur à une partie
* **Chaos test**: routes/chaos_test.go pour tester la résistance du serveur à la charge en faisant jouer 50 bots en même temps

Pour faire fonctionner ces tests, notamment chaos et end to end, le client est nécessaire.

### Règles du jeu

D'autres information sur le jeu se trouvent dans le fichier PROTOCOL.md

---

**Auteur :** Elisa Donet - Eliott Houcke
**Projet :** Système Répartis - ESIR

```


