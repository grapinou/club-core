-- Refresh only additive public content in an existing Budokan demo.
UPDATE organizations SET
 public_phone_label=coalesce(public_phone_label,'Seb Colosse'),
 trial_session_description=coalesce(trial_session_description,'La séance comprend généralement un échauffement, une partie technique puis des combats. Un gradé vous accompagne pour découvrir l’activité et répondre à vos questions.'),
 trial_items_to_bring=CASE WHEN cardinality(trial_items_to_bring)=0 THEN ARRAY['De l’eau','Une tenue de sport confortable','Des claquettes pour circuler jusqu’au dojo'] ELSE trial_items_to_bring END,
 trial_equipment_offer=coalesce(trial_equipment_offer,'Un kimono peut être prêté selon les disponibilités.'),
 trial_equipment_detail_prompt=coalesce(trial_equipment_detail_prompt,'Indiquez votre taille afin que nous puissions prévoir un kimono adapté, selon les disponibilités.')
WHERE name='Budokan Sud Oise' AND public_email='budokansud.oise@gmail.com';

INSERT INTO organization_public_images (organization_id,placement,src,webp_srcset,alt,width,height)
SELECT o.id,media.placement,media.src,media.webp_srcset,media.alt,media.width,media.height FROM organizations o CROSS JOIN (VALUES
 ('hero','/static/images/budokan/hero/hero-bureau-mascots.png','/static/images/budokan/hero/hero-bureau-mascots-800.webp 800w, /static/images/budokan/hero/hero-bureau-mascots-1600.webp 1600w','Les mascottes du club en tenue de jiu-jitsu brésilien',1774,887),
 ('activity','/static/images/budokan/illustrations/training-jjb.png','/static/images/budokan/illustrations/training-jjb-800.webp 800w, /static/images/budokan/illustrations/training-jjb-1536.webp 1536w','',1536,1024),
 ('community','/static/images/budokan/illustrations/club-spirit.png','/static/images/budokan/illustrations/club-spirit-800.webp 800w, /static/images/budokan/illustrations/club-spirit-1536.webp 1536w','',1536,1024),
 ('schedule','/static/images/budokan/illustrations/schedule-jjb.png','/static/images/budokan/illustrations/schedule-jjb-800.webp 800w, /static/images/budokan/illustrations/schedule-jjb-1536.webp 1536w','',1536,1024),
 ('trial','/static/images/budokan/illustrations/trial-jjb.png','/static/images/budokan/illustrations/trial-jjb-800.webp 800w, /static/images/budokan/illustrations/trial-jjb-1536.webp 1536w','',1536,1024)
) AS media(placement,src,webp_srcset,alt,width,height)
WHERE o.name='Budokan Sud Oise' AND o.public_email='budokansud.oise@gmail.com'
ON CONFLICT (organization_id,placement) DO NOTHING;

-- Preserve historical trials tied to the old technical slot. Publish a new
-- slot on the real public group, then retire the old slot from future offers.
INSERT INTO group_slots (group_id,season_id,weekday,start_time,end_time,location,location_id,practice_label,valid_from,valid_until,is_active)
SELECT public_group.id, old.season_id,old.weekday,old.start_time,old.end_time,old.location,old.location_id,old.practice_label,old.valid_from,old.valid_until,true
FROM group_slots old JOIN groups technical ON technical.id=old.group_id
JOIN groups public_group ON public_group.activity_id=technical.activity_id AND public_group.name='JJB Adolescents et Adultes'
WHERE technical.name='JJB pratiques spécifiques' AND old.weekday=1 AND old.start_time='20:15' AND old.end_time='22:00'
AND old.practice_label='Préparation physique / Jiu-Jitsu Brésilien' AND old.is_active
AND NOT EXISTS (SELECT 1 FROM group_slots existing WHERE existing.group_id=public_group.id AND existing.season_id=old.season_id AND existing.weekday=old.weekday AND existing.start_time=old.start_time AND existing.end_time=old.end_time AND existing.practice_label=old.practice_label);

UPDATE group_slots old SET is_active=false,updated_at=NOW()
FROM groups technical WHERE old.group_id=technical.id AND technical.name='JJB pratiques spécifiques'
AND old.weekday=1 AND old.start_time='20:15' AND old.end_time='22:00' AND old.practice_label='Préparation physique / Jiu-Jitsu Brésilien' AND old.is_active
AND EXISTS (SELECT 1 FROM groups public_group JOIN group_slots new_slot ON new_slot.group_id=public_group.id WHERE public_group.activity_id=technical.activity_id AND public_group.name='JJB Adolescents et Adultes' AND new_slot.season_id=old.season_id AND new_slot.weekday=old.weekday AND new_slot.start_time=old.start_time AND new_slot.end_time=old.end_time AND new_slot.practice_label=old.practice_label AND new_slot.is_active);
