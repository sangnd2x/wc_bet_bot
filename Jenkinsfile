pipeline {
    agent any

    environment {
        WORLDCUP_BOT_TELEGRAM_TOKEN     = credentials('WORLDCUP_BOT_TELEGRAM_TOKEN')
        WORLDCUP_BOT_ADMIN_TELEGRAM_ID  = credentials('WORLDCUP_BOT_ADMIN_TELEGRAM_ID')
        WORLDCUP_BOT_GROUP_CHAT_ID      = credentials('WORLDCUP_BOT_GROUP_CHAT_ID')
        WORLDCUP_BOT_USER1_TELEGRAM_ID  = credentials('WORLDCUP_BOT_USER1_TELEGRAM_ID')
        WORLDCUP_BOT_USER2_TELEGRAM_ID  = credentials('WORLDCUP_BOT_USER2_TELEGRAM_ID')
        TELEGRAM_BOT_TOKEN              = credentials('TELEGRAM_BOT_TOKEN')
        TELEGRAM_CHAT_ID                = credentials('TELEGRAM_CHAT_ID')
        FOOTBALL_API_KEY                = credentials('FOOTBALL_API_KEY')
        SHEETS_SPREADSHEET_ID           = credentials('SHEETS_SPREADSHEET_ID')

        COMPETITION_CODE                = 'WC'
        SEASON                          = '2026'
        POLL_INTERVAL_MIN               = '15'
        SHEETS_TAB_NAME                 = 'Bets'
        DB_PATH                         = '/data/bets.db'
        DAILY_BROADCAST_CRON            = '0 8 * * *'
        TIMEZONE                        = 'Asia/Ho_Chi_Minh'
    }

    stages {
        stage('Checkout') {
            steps {
                checkout scm
            }
        }

        stage('Create .env') {
            steps {
                withCredentials([file(credentialsId: 'GOOGLE_CREDENTIALS_JSON', variable: 'GOOGLE_CREDS_FILE')]) {
                    sh '''
                        cat > .env << EOF
TELEGRAM_TOKEN=${WORLDCUP_BOT_TELEGRAM_TOKEN}
GROUP_CHAT_ID=${WORLDCUP_BOT_GROUP_CHAT_ID}
ADMIN_TELEGRAM_ID=${WORLDCUP_BOT_ADMIN_TELEGRAM_ID}
FOOTBALL_API_KEY=${FOOTBALL_API_KEY}
COMPETITION_CODE=${COMPETITION_CODE}
SEASON=${SEASON}
POLL_INTERVAL_MIN=${POLL_INTERVAL_MIN}
SHEETS_SPREADSHEET_ID=${SHEETS_SPREADSHEET_ID}
SHEETS_TAB_NAME=${SHEETS_TAB_NAME}
DB_PATH=${DB_PATH}
DAILY_BROADCAST_CRON=${DAILY_BROADCAST_CRON}
TIMEZONE=${TIMEZONE}
USER1_TELEGRAM_ID=${WORLDCUP_BOT_USER1_TELEGRAM_ID}
USER2_TELEGRAM_ID=${WORLDCUP_BOT_USER2_TELEGRAM_ID}
EOF
                        echo "GOOGLE_CREDENTIALS_B64=$(base64 -w 0 $GOOGLE_CREDS_FILE)" >> .env
                    '''
                }
            }
        }

        stage('Build & Deploy') {
            steps {
                sh '''
                    docker compose down --remove-orphans
                    docker compose up -d --build
                '''
            }
        }

        stage('Cleanup') {
            steps {
                sh 'docker image prune -f'
            }
        }
    }

    post {
        always {
            sh 'rm -f .env'
        }
        success {
            sh """
                curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
                    -d chat_id="${TELEGRAM_CHAT_ID}" \
                    -d parse_mode="Markdown" \
                    -d text="✅ *worldcup-bet-bot deployed*%0ARepo: ${env.JOB_NAME}%0ABranch: ${env.GIT_BRANCH}%0ACommit: ${env.GIT_COMMIT.take(7)}%0AView run: ${env.BUILD_URL}"
            """
        }
        failure {
            sh """
                curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
                    -d chat_id="${TELEGRAM_CHAT_ID}" \
                    -d parse_mode="Markdown" \
                    -d text="❌ *worldcup-bet-bot deploy failed*%0ARepo: ${env.JOB_NAME}%0ABranch: ${env.GIT_BRANCH}%0ACommit: ${env.GIT_COMMIT.take(7)}%0AView run: ${env.BUILD_URL}"
            """
        }
    }
}
