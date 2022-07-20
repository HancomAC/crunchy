import * as util from 'util';
import {exec, spawn} from 'child_process'

const {beta, image, svc} = require('args-parser')(process.argv);
const exec_p = util.promisify(exec);
const buildBeginTime = Date.now()

function getElapsedTime() {
    return (Date.now() - buildBeginTime) / 1000
}

function getDigest(stdout) {
    return stdout.toString().split('\n').slice(-2)[0].split('digest: ')[1].split(' ')[0]
}

function run(proc: string, options: string[]) {
    return new Promise<void>((resolve) => {
        // @ts-ignore
        const _ = spawn(proc, options, {cwd: process.cwd()});
        _.on('close', () => {
            resolve();
        })
    })
}

async function main() {
    console.log(`Building... (${getElapsedTime()}s)`)
    await run('npm', ['run', 'build:docker']);

    console.log(`Uploading image... (${getElapsedTime()}s)`)
    const result_main = getDigest((await exec_p('docker push asia.gcr.io/hancomac/' + image + (beta ? ':dev' : ''))).stdout);

    const rv = svc.split(';');
    const prs = [];
    console.log(`Deploying... (${getElapsedTime()}s)`)
    for (let i = 0; i < rv.length; i++) {
        prs.push((async () => {
            const rev = (await exec_p(`gcloud run revisions list --region=asia-northeast3 --service=${rv[i]}`)).stdout.toString().split('\n').slice(1).map(el => el.replace('  ', ' ').split(' ')[1]).filter(x => x)
            await run('gcloud', ['run', 'deploy', rv[i], `--image=asia.gcr.io/hancomac/${image}@${result_main}`,
                '--platform=managed', '--region=asia-northeast3', '--project=hancomac'])
            console.log(`Migrating ${rv[i]}... (${getElapsedTime()}s)`);
            await run('gcloud', ['run', 'services', 'update-traffic', rv[i], '--to-latest', '--region=asia-northeast3']);
            console.log(`Deployed ${rv[i]}. (${getElapsedTime()}s)`);
            await Promise.all(rev.slice(1).map(async (el) => {
                console.log(`Deleting ${el}... (${getElapsedTime()}s)`);
                await run('gcloud', ['run', 'revisions', 'delete', el, '--region=asia-northeast3', '-q']);
                console.log(`Deleted ${el}. (${getElapsedTime()}s)`);
            }));
        })());
    }
    await Promise.all(prs);

    if (!beta) {
        console.log(`Cleaning up image... (${getElapsedTime()}s)`);
        const image_old = JSON.parse((await exec_p(`gcloud container images list-tags asia.gcr.io/hancomac/${image} --format=json`)).stdout.toString()).slice(2);
        await Promise.all(image_old.map(async (el) => {
            await run('gcloud', ['container', 'images', 'delete', `asia.gcr.io/hancomac/${image}@${el.digest}`, '--force-delete-tags', '-q']);
            console.log(`Deleted asia.gcr.io/hancomac/${image}@${el.digest}. (${getElapsedTime()}s)`);
        }));
    }
}

main().then(() => {
    console.log(`⚡ Done in ${getElapsedTime()}s`);
}).catch(err => {
    console.error(err);
    process.exit(1);
});
