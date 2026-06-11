pipeline {
    agent any

    environment {
        WORLDCUP_BOT_TELEGRAM_TOKEN     = credentials('WORLDCUP_BOT_TELEGRAM_TOKEN')
        WORLDCUP_BOT_ADMIN_TELEGRAM_ID  = credentials('WORLDCUP_BOT_ADMIN_TELEGRAM_ID')
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
            node('') {
                sh 'rm -f .env'
            }
        }
        success {
            withCredentials([
                string(credentialsId: 'TELEGRAM_BOT_TOKEN', variable: 'BOT_TOKEN'),
                string(credentialsId: 'TELEGRAM_CHAT_ID', variable: 'CHAT_ID')
            ]) {
                sh """
                    curl -s -X POST "https://api.telegram.org/bot\${BOT_TOKEN}/sendMessage" \
                        -d chat_id="\${CHAT_ID}" \
                        -d parse_mode="Markdown" \
                        -d text="✅ *worldcup-bet-bot deployed*%0ARepo: ${env.JOB_NAME}%0ABranch: ${env.GIT_BRANCH}%0ACommit: ${(env.GIT_COMMIT ?: 'unknown').take(7)}%0AView run: ${env.BUILD_URL}"
                """
            }
        }
        failure {
            withCredentials([
                string(credentialsId: 'TELEGRAM_BOT_TOKEN', variable: 'BOT_TOKEN'),
                string(credentialsId: 'TELEGRAM_CHAT_ID', variable: 'CHAT_ID')
            ]) {
                sh """
                    curl -s -X POST "https://api.telegram.org/bot\${BOT_TOKEN}/sendMessage" \
                        -d chat_id="\${CHAT_ID}" \
                        -d parse_mode="Markdown" \
                        -d text="❌ *worldcup-bet-bot deploy failed*%0ARepo: ${env.JOB_NAME}%0ABranch: ${env.GIT_BRANCH}%0ACommit: ${(env.GIT_COMMIT ?: 'unknown').take(7)}%0AView run: ${env.BUILD_URL}"
                """
            }
        }
    }
}
